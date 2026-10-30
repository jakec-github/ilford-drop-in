import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import OverridesCard, { OverrideCard } from "./OverridesCard";
import type {
  ConfiguredRole,
  Override,
  OverrideDraft,
  OverrideRuleReading,
  Volunteer,
} from "../types";

// Roles named unlike any real Organisation's: the editor draws whatever it is
// given.
const ROLES: ConfiguredRole[] = [
  { id: "r-duty", name: "Duty lead", priority: 0, colour: "violet" },
  { id: "r-greet", name: "Greeter", priority: 1, colour: "teal" },
];

function volunteer(id: string, name: string, roles: string[]): Volunteer {
  return {
    id,
    name,
    fullName: name,
    roles,
    group: null,
    gender: null,
    active: true,
  };
}

const VOLUNTEERS = [
  volunteer("v-ann", "Ann", ["Duty lead", "Greeter"]),
  volunteer("v-bo", "Bo", ["Greeter"]),
];

// The server's reading of whatever rule is typed. The editor asks it rather
// than reading the rule itself.
let reading: OverrideRuleReading = {
  granularity: "date",
  description: "On the first Sunday of every month",
  problems: [],
};
const realFetch = globalThis.fetch;
const fetchMock = mock(() =>
  Promise.resolve(new Response(JSON.stringify(reading))),
);

beforeEach(() => {
  fetchMock.mockClear();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

// Long enough for the rule to settle and be read.
const settle = () =>
  act(() => new Promise<void>((resolve) => setTimeout(resolve, 400)));
// Long enough for a change to stop moving and be saved.
const saveWait = () =>
  act(() => new Promise<void>((resolve) => setTimeout(resolve, 1000)));

const STORED: Override = {
  id: "o-1",
  number: 1,
  rrule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=MONTHLY;BYDAY=1SU",
  granularity: "date",
  description: "On the first Sunday of every month",
  problems: [],
  startTime: null,
  lengthMinutes: null,
  shape: null,
  pins: [],
};

function renderCard(override: Override = STORED) {
  const onSave = mock<(draft: OverrideDraft) => Promise<void>>(() =>
    Promise.resolve(),
  );
  render(
    <OverrideCard
      override={override}
      number={1}
      count={1}
      roles={ROLES}
      defaultShape={[{ roleId: "r-greet", role: "Greeter", count: 2 }]}
      volunteers={VOLUNTEERS}
      volunteersError={null}
      fresh={false}
      busy={false}
      held={false}
      onSave={onSave}
      onRemove={() => Promise.resolve()}
      onGrab={() => {}}
      onMove={() => {}}
    />,
  );
  return onSave;
}

function pin(who: string, roleId?: string) {
  fireEvent.change(screen.getByLabelText("Who to pin"), {
    target: { value: who },
  });
  if (roleId) {
    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: roleId },
    });
  }
  fireEvent.click(screen.getByRole("button", { name: "Pin" }));
}

describe("OverrideCard", () => {
  beforeEach(() => {
    reading = {
      granularity: "date",
      description: "On the first Sunday of every month",
      problems: [],
    };
  });

  test("reads the rule back as the server describes it", async () => {
    renderCard();
    await settle();
    expect(screen.getByText("On the first Sunday of every month")).toBeTruthy();
    expect(screen.getByText("every shift that day")).toBeTruthy();
  });

  // The date it counts from is part of the rule (ADR 0014), so there is no
  // field for it of its own.
  test("the rule carries its own start date", () => {
    renderCard();
    const matches = screen.getByRole("region", { name: "Override 1 matches" });
    const rule = within(matches).getByLabelText(
      "Override 1 rule",
    ) as HTMLTextAreaElement;
    expect(rule.value).toContain("DTSTART;VALUE=DATE:20261101");
    expect(screen.queryByLabelText("Override 1 start from")).toBeNull();
  });

  test("nothing is saved until something changes", async () => {
    const onSave = renderCard();
    await saveWait();
    expect(onSave).not.toHaveBeenCalled();
  });

  test("a change saves itself, leaving the default shape alone", async () => {
    const onSave = renderCard();
    await settle();
    pin("v-bo");
    pin("custom");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Youth group" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Pin" }));
    await saveWait();

    expect(onSave).toHaveBeenCalledTimes(1);
    const draft = onSave.mock.calls[0][0];
    expect(draft.rrule).toBe(
      "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=MONTHLY;BYDAY=1SU",
    );
    expect(draft.shape).toBeNull();
    expect(draft.pins).toEqual([
      { roleId: "r-greet", person: { volunteerId: "v-bo" } },
      { roleId: "r-duty", person: { custom: "Youth group" } },
    ]);
  });

  // A person fills one Seat on a Shift, so pinning them again changes their
  // Role rather than pinning them twice.
  test("pinning somebody again replaces their pin", async () => {
    const onSave = renderCard();
    await settle();
    pin("v-ann", "r-duty");
    pin("v-ann", "r-greet");
    await saveWait();

    expect(onSave.mock.calls.at(-1)![0].pins).toEqual([
      { roleId: "r-greet", person: { volunteerId: "v-ann" } },
    ]);
  });

  test("ticking the shape starts it from the default", async () => {
    const onSave = renderCard();
    await settle();
    fireEvent.click(screen.getByLabelText("Shape"));
    await saveWait();
    expect(onSave.mock.calls[0][0].shape).toEqual([
      { roleId: "r-greet", count: 2 },
    ]);
  });

  test("a ticked start time is saved only once it is said", async () => {
    const onSave = renderCard();
    await settle();
    fireEvent.click(screen.getByLabelText("Start time"));
    await saveWait();
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Override 1 start time"), {
      target: { value: "19:00" },
    });
    fireEvent.click(screen.getByLabelText("Length"));
    fireEvent.change(screen.getByLabelText("Override 1 length"), {
      target: { value: "150" },
    });
    await saveWait();
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0].startTime).toBe("19:00");
    expect(onSave.mock.calls[0][0].lengthMinutes).toBe(150);
  });

  test("a rule the server cannot read is not saved", async () => {
    const onSave = renderCard();
    await settle();
    reading = {
      granularity: "",
      description: "",
      problems: ["FREQ=SOMETIMES is not a frequency"],
    };
    fireEvent.change(screen.getByLabelText("Override 1 rule"), {
      target: { value: "FREQ=SOMETIMES" },
    });
    await saveWait();
    expect(screen.getByText("FREQ=SOMETIMES is not a frequency")).toBeTruthy();
    expect(onSave).not.toHaveBeenCalled();
  });

  test("removing asks first, on the card", () => {
    renderCard();
    fireEvent.click(screen.getByLabelText("Remove override 1"));
    expect(screen.getByText("Remove this override?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep" }));
    expect(screen.queryByText("Remove this override?")).toBeNull();
  });
});

// The card as a whole, against a server that holds three Overrides and answers
// a reorder with them in the order it was sent.
describe("OverridesCard reordering", () => {
  function saved(id: string, day: string, description: string): Override {
    return {
      id,
      number: 0,
      rrule: `DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=${day}`,
      granularity: "date",
      description,
      problems: [],
      startTime: null,
      lengthMinutes: null,
      shape: null,
      pins: [],
    };
  }
  const LIST = [
    saved("o-a", "SU", "First"),
    saved("o-b", "MO", "Second"),
    saved("o-c", "TU", "Third"),
  ];
  const numbered = (ids: string[]) =>
    ids.map((id, i) => ({
      ...LIST.find((o) => o.id === id)!,
      number: i + 1,
    }));

  let sent: string[][] = [];
  const json = (body: unknown) =>
    Promise.resolve(new Response(JSON.stringify(body)));
  const server = mock((url: string, init?: RequestInit) => {
    if (url.endsWith("/api/roles")) {
      return json({ roles: ROLES.map((r) => ({ ...r })) });
    }
    if (url.endsWith("/api/volunteers")) return json({ volunteers: [] });
    if (url.endsWith("/overrides/reading")) {
      const { rrule } = JSON.parse(String(init?.body)) as { rrule: string };
      const o = LIST.find((x) => x.rrule === rrule)!;
      return json({ ...o, problems: [] });
    }
    if (url.endsWith("/api/rota-defaults")) {
      return json({
        shiftPattern: "",
        patternEntries: [],
        shiftTimezone: "",
        defaultShape: [],
        allocationRules: [],
        ruleLibrary: [],
      });
    }
    if (url.endsWith("/overrides/order")) {
      const { ids } = JSON.parse(String(init?.body)) as { ids: string[] };
      sent.push(ids);
      return json({ overrides: numbered(ids) });
    }
    return json({ overrides: numbered(LIST.map((o) => o.id)) });
  });

  beforeEach(() => {
    sent = [];
    globalThis.fetch = server as unknown as typeof fetch;
  });

  const descriptions = () =>
    screen
      .getAllByRole("article")
      .map((a) => a.querySelector(".override-description")?.textContent);

  test("an arrow key on the handle moves an override a place", async () => {
    render(<OverridesCard />);
    await settle();
    fireEvent.keyDown(screen.getByLabelText("Reorder override 1"), {
      key: "ArrowDown",
    });
    await settle();
    expect(sent).toEqual([["o-b", "o-a", "o-c"]]);
    expect(descriptions()[0]).toContain("Second");
  });

  test("dragging the handle past others drops it there, written once", async () => {
    render(<OverridesCard />);
    await settle();
    // Each item 100px tall, stacked from the top of the list, in the order
    // they are drawn.
    const items = () =>
      Array.from(document.querySelectorAll("ol.drag-list > li"));
    const realBox = HTMLElement.prototype.getBoundingClientRect;
    HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
      const i = items().indexOf(this as HTMLLIElement);
      return { top: Math.max(i, 0) * 100, height: 100 } as DOMRect;
    };
    const realCapture = HTMLElement.prototype.setPointerCapture;
    const capture = mock(() => {});
    HTMLElement.prototype.setPointerCapture = capture;
    try {
      const list = document.querySelector("ol.drag-list")!;
      fireEvent.pointerDown(screen.getByLabelText("Reorder override 1"), {
        button: 0,
        pointerId: 1,
        clientY: 50,
      });
      fireEvent.pointerMove(list, { pointerId: 1, clientY: 180 });
      fireEvent.pointerMove(list, { pointerId: 1, clientY: 280 });
      // While held it follows the pointer, the two it passed slide up a
      // place, and each is numbered as the drop would leave it — but nothing
      // is written, and the list itself keeps its order.
      const slid = items().map((li) => (li as HTMLElement).style.transform);
      expect(slid).toEqual([
        "translateY(230px)",
        "translateY(-100px)",
        "translateY(-100px)",
      ]);
      expect(screen.getByLabelText("Override 3").textContent).toContain(
        "First",
      );
      expect(descriptions()[0]).toContain("First");
      expect(sent).toEqual([]);
      fireEvent.pointerUp(list, { pointerId: 1, clientY: 280 });
      await settle();
    } finally {
      HTMLElement.prototype.getBoundingClientRect = realBox;
      HTMLElement.prototype.setPointerCapture = realCapture;
    }
    expect(capture).toHaveBeenCalled();
    expect(sent).toEqual([["o-b", "o-c", "o-a"]]);
    expect(descriptions()).toEqual([
      expect.stringContaining("Second"),
      expect.stringContaining("Third"),
      expect.stringContaining("First"),
    ]);
  });
});
