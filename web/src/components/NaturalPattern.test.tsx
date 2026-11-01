import { describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen } from "@testing-library/react";
import NaturalPattern from "./NaturalPattern";
import {
  MAX_DESCRIPTION,
  type PatternEntry,
  type ShiftPatternTranslation,
} from "../types";

const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 0)));

const SUNDAYS = "BEGIN:VEVENT\nDTSTART:20261101T193000\nEND:VEVENT";
const sundayEntry: PatternEntry = {
  number: 1,
  line: 1,
  description: "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026",
  problems: [],
  warnings: [],
};

type Translate = (text: string) => Promise<ShiftPatternTranslation>;

function renderPattern({
  descriptions = [] as string[],
  translate = mock<Translate>(() =>
    Promise.resolve({ status: "ok", pattern: SUNDAYS, entries: [sundayEntry] }),
  ),
  onPattern = mock<(pattern: string, entries: PatternEntry[]) => void>(),
  onUseRaw = mock<() => void>(),
} = {}) {
  const view = render(
    <NaturalPattern
      descriptions={descriptions}
      translate={translate}
      onPattern={onPattern}
      onUseRaw={onUseRaw}
    />,
  );
  return { ...view, translate, onPattern, onUseRaw };
}

const input = () =>
  screen.getByPlaceholderText(
    "Describe the shift pattern in plain English",
  ) as HTMLTextAreaElement;
const generate = () =>
  screen.getByRole("button", { name: /Generat/ }) as HTMLButtonElement;
const type = (text: string) =>
  fireEvent.change(input(), { target: { value: text } });

describe("NaturalPattern", () => {
  test("with no pattern, offers an input and Generate once there is text", () => {
    renderPattern();
    expect(input().value).toBe("");
    expect(generate().disabled).toBe(true);
    type("Sundays 7:30 to 10:30pm");
    expect(generate().disabled).toBe(false);
  });

  test("a generated pattern is handed up, the input read-only meanwhile", async () => {
    let answer!: (t: ShiftPatternTranslation) => void;
    const translate = mock<Translate>(
      () => new Promise((resolve) => (answer = resolve)),
    );
    const { onPattern } = renderPattern({ translate });
    type("Sundays 7:30 to 10:30pm");
    fireEvent.click(generate());

    expect(translate.mock.calls).toEqual([["Sundays 7:30 to 10:30pm"]]);
    expect(generate().textContent).toBe("Generating…");
    expect(generate().disabled).toBe(true);
    expect(input().readOnly).toBe(true);

    await act(async () =>
      answer({ status: "ok", pattern: SUNDAYS, entries: [sundayEntry] }),
    );
    expect(onPattern.mock.calls).toEqual([[SUNDAYS, [sundayEntry]]]);
  });

  test("issues are listed under the input, and the same text is not tried twice", async () => {
    const translate = mock<Translate>(() =>
      Promise.resolve({
        status: "issues",
        issues: [
          {
            kind: "missing",
            message: "Add how long the Sunday shifts run",
            examples: [],
          },
          {
            kind: "ambiguous",
            message: '"Evenings" — at what time?',
            examples: ["7pm", "7:30pm"],
          },
        ],
      }),
    );
    const { onPattern } = renderPattern({ translate });
    type("Sunday evenings");
    fireEvent.click(generate());
    await settle();

    expect(screen.getByText("Add how long the Sunday shifts run")).toBeTruthy();
    expect(screen.getByText('"Evenings" — at what time?')).toBeTruthy();
    expect(screen.getByText("7pm")).toBeTruthy();
    expect(screen.getByText("7:30pm")).toBeTruthy();
    expect(onPattern).not.toHaveBeenCalled();

    expect(generate().disabled).toBe(true);
    type("Sunday evenings at 7pm for 3 hours");
    expect(generate().disabled).toBe(false);
  });

  test("a failed Generate says so and offers raw entry", async () => {
    const translate = mock<Translate>(() =>
      Promise.reject(new Error("Couldn't translate just now")),
    );
    const { onPattern, onUseRaw } = renderPattern({ translate });
    type("Sundays");
    fireEvent.click(generate());
    await settle();

    expect(screen.getByText(/Couldn't translate just now/)).toBeTruthy();
    expect(onPattern).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /write the entries/i }));
    expect(onUseRaw).toHaveBeenCalledTimes(1);
  });

  test("past the limit a counter shows and Generate is refused", () => {
    renderPattern();
    type("a".repeat(MAX_DESCRIPTION - 50));
    expect(
      screen.getByText(`${MAX_DESCRIPTION - 50} / ${MAX_DESCRIPTION}`),
    ).toBeTruthy();
    expect(generate().disabled).toBe(false);
    type("a".repeat(MAX_DESCRIPTION + 1));
    expect(
      screen.getByText(`${MAX_DESCRIPTION + 1} / ${MAX_DESCRIPTION}`),
    ).toBeTruthy();
    expect(generate().disabled).toBe(true);
  });

  test("no counter well short of the limit", () => {
    renderPattern();
    type("Sundays");
    expect(screen.queryByText(new RegExp(`/ ${MAX_DESCRIPTION}`))).toBeNull();
  });

  test("a pattern reads as its description, which reopens the input pre-filled", () => {
    renderPattern({
      descriptions: [
        "Every Sunday at 19:30 for 3 hours",
        "Every weekday at 09:00 for 3 hours",
      ],
    });
    expect(
      screen.queryByPlaceholderText(
        "Describe the shift pattern in plain English",
      ),
    ).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: /Every Sunday at 19:30/ }),
    );
    expect(input().value).toBe(
      "Every Sunday at 19:30 for 3 hours\nEvery weekday at 09:00 for 3 hours",
    );

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.queryByPlaceholderText(
        "Describe the shift pattern in plain English",
      ),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: /Every Sunday at 19:30/ }),
    ).toBeTruthy();
  });

  test("a generated pattern closes the input onto its description", async () => {
    const { rerender, translate, onPattern, onUseRaw } = renderPattern({
      descriptions: ["Every Monday at 10:00 for 2 hours"],
    });
    fireEvent.click(screen.getByRole("button", { name: /Every Monday/ }));
    type("Sundays 7:30pm for 3 hours");
    fireEvent.click(generate());
    await settle();

    rerender(
      <NaturalPattern
        descriptions={[sundayEntry.description]}
        translate={translate}
        onPattern={onPattern}
        onUseRaw={onUseRaw}
      />,
    );
    expect(
      screen.getByRole("button", { name: new RegExp(sundayEntry.description) }),
    ).toBeTruthy();
    expect(
      screen.queryByPlaceholderText(
        "Describe the shift pattern in plain English",
      ),
    ).toBeNull();
  });
});
