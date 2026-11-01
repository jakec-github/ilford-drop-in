import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { RotaDefaultsForm } from "./RotaDefaultsCard";
import type {
  NaturalEditor,
  PatternEntry,
  RotaDefaults,
  ShiftPatternTranslation,
} from "../types";

const SUNDAYS =
  "BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT";
const sundayEntry: PatternEntry = {
  number: 1,
  line: 1,
  description: "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026",
  problems: [],
  warnings: [],
};

// The server's reading of a pattern: Sundays read clean, anything else has a
// problem.
const realFetch = globalThis.fetch;
const server = mock((_url: string, init?: RequestInit) => {
  const { shiftPattern } = JSON.parse(String(init?.body)) as {
    shiftPattern: string;
  };
  const clean = shiftPattern === SUNDAYS;
  return Promise.resolve(
    new Response(
      JSON.stringify({
        entries: [
          clean
            ? sundayEntry
            : { ...sundayEntry, description: "", problems: ["no DTSTART"] },
        ],
        problems: [],
        warnings: [],
        startsFrom: clean ? "2026-11-01" : "",
        starts: [],
      }),
    ),
  );
});

beforeEach(() => {
  server.mockClear();
  globalThis.fetch = server as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

// Long enough for a pattern to be read. React applies what happened inside
// an act only as it ends, so each step after that — the reading, then the save
// that waits on it — takes another.
const settle = () =>
  act(() => new Promise<void>((resolve) => setTimeout(resolve, 700)));

function defaultsWith(
  naturalEditor: NaturalEditor,
  shiftPattern = "",
): RotaDefaults {
  return {
    shiftPattern,
    patternEntries: shiftPattern ? [sundayEntry] : [],
    shiftTimezone: "Europe/London",
    defaultShape: [],
    allocationRules: [],
    ruleLibrary: [],
    naturalEditor,
  };
}

function renderForm(
  defaults: RotaDefaults,
  translate = mock<(text: string) => Promise<ShiftPatternTranslation>>(() =>
    Promise.resolve({ status: "ok", pattern: SUNDAYS, entries: [sundayEntry] }),
  ),
) {
  const onSavePattern = mock<(pattern: string) => Promise<void>>(() =>
    Promise.resolve(),
  );
  render(
    <RotaDefaultsForm
      defaults={defaults}
      roles={[]}
      onState={() => {}}
      onSavePattern={onSavePattern}
      onSaveShape={() => Promise.resolve()}
      translate={translate}
    />,
  );
  return { onSavePattern, translate };
}

const option = (name: string) =>
  screen.getByRole("button", { name }) as HTMLButtonElement;
const switchGroup = () =>
  screen.getByRole("group", { name: "How the pattern is written" });
const description = () =>
  screen.queryByPlaceholderText(
    "Describe the shift pattern in plain English",
  ) as HTMLTextAreaElement | null;
const raw = () =>
  screen.queryByPlaceholderText(/BEGIN:VEVENT/) as HTMLTextAreaElement | null;

describe("RotaDefaultsForm", () => {
  test("opens in plain English where it is available", () => {
    renderForm(defaultsWith({ available: true }));
    expect(option("Natural").getAttribute("aria-pressed")).toBe("true");
    expect(description()).toBeTruthy();
    expect(raw()).toBeNull();
  });

  test("opens raw, with the switch locked and the reason on hover, where it is not", () => {
    renderForm(
      defaultsWith({ available: false, reason: "Not turned on here" }),
    );
    expect(raw()).toBeTruthy();
    expect(description()).toBeNull();
    expect(option("Natural").disabled).toBe(true);
    expect(option("Raw").disabled).toBe(true);
    expect(switchGroup().getAttribute("title")).toBe("Not turned on here");
  });

  test("a raw edit with problems locks the switch until it is undone", async () => {
    renderForm(defaultsWith({ available: true }, SUNDAYS));
    fireEvent.click(option("Raw"));
    fireEvent.change(raw()!, { target: { value: "BEGIN:VEVENT" } });
    await settle();
    expect(option("Natural").disabled).toBe(true);
    expect(switchGroup().getAttribute("title")).toBe(
      "Fix or undo the raw edit first",
    );

    fireEvent.change(raw()!, { target: { value: SUNDAYS } });
    await settle();
    expect(option("Natural").disabled).toBe(false);
    expect(switchGroup().getAttribute("title")).toBeNull();
  });

  test("a generated pattern is saved as a typed one would be, and described", async () => {
    const { onSavePattern, translate } = renderForm(
      defaultsWith({ available: true }),
    );
    fireEvent.change(description()!, {
      target: { value: "Sundays 7:30 to 10:30pm" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));
    await settle();
    await settle();
    await settle();

    expect(translate.mock.calls).toEqual([["Sundays 7:30 to 10:30pm"]]);
    expect(onSavePattern.mock.calls).toEqual([[SUNDAYS]]);
    expect(
      screen.getByRole("button", { name: new RegExp(sundayEntry.description) }),
    ).toBeTruthy();
  });

  test("a failed Generate's way out is the raw editor", async () => {
    renderForm(
      defaultsWith({ available: true }),
      mock(() => Promise.reject(new Error("Couldn't translate just now"))),
    );
    fireEvent.change(description()!, { target: { value: "Sundays" } });
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));
    await settle();

    fireEvent.click(
      screen.getByRole("button", { name: "Write the entries yourself" }),
    );
    expect(raw()).toBeTruthy();
    expect(option("Raw").getAttribute("aria-pressed")).toBe("true");
  });
});
