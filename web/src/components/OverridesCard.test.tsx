import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen } from "@testing-library/react";
import OverridesCard, { OverrideEditor } from "./OverridesCard";
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
  startFrom: "2026-11-01",
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

function renderEditor(override: Override | null = null) {
  const onSave = mock<(draft: OverrideDraft) => Promise<void>>(() =>
    Promise.resolve(),
  );
  render(
    <OverrideEditor
      number={1}
      override={override}
      roles={ROLES}
      volunteers={VOLUNTEERS}
      volunteersError={null}
      onSave={onSave}
      onCancel={() => {}}
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

describe("OverrideEditor", () => {
  test("reads the rule back as the server describes it", async () => {
    renderEditor();
    await settle();
    expect(screen.getByText("On the first Sunday of every month")).toBeTruthy();
    expect(screen.getByText("every shift that day")).toBeTruthy();
  });

  test("leaves the default shape unless one is ticked, and sends the pins", async () => {
    const onSave = renderEditor();
    await settle();
    pin("v-bo");
    pin("custom");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Youth group" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Pin" }));

    fireEvent.click(screen.getByRole("button", { name: "Add override" }));
    await settle();

    const draft = onSave.mock.calls[0][0];
    expect(draft.rrule).toBe("FREQ=MONTHLY;BYDAY=1SU");
    expect(draft.shape).toBeNull();
    expect(draft.pins).toEqual([
      { roleId: "r-greet", person: { volunteerId: "v-bo" } },
      { roleId: "r-duty", person: { custom: "Youth group" } },
    ]);
  });

  // A person fills one Seat on a Shift, so pinning them again changes their
  // Role rather than pinning them twice.
  test("pinning somebody again replaces their pin", async () => {
    const onSave = renderEditor();
    await settle();
    pin("v-ann", "r-duty");
    pin("v-ann", "r-greet");
    fireEvent.click(screen.getByRole("button", { name: "Add override" }));
    await settle();

    expect(onSave.mock.calls[0][0].pins).toEqual([
      { roleId: "r-greet", person: { volunteerId: "v-ann" } },
    ]);
  });

  test("a ticked shape is sent whole, and may ask for nobody", async () => {
    const onSave = renderEditor();
    await settle();
    fireEvent.click(screen.getByLabelText("Replace the shape"));
    fireEvent.click(screen.getByRole("button", { name: "Add override" }));
    await settle();
    expect(onSave.mock.calls[0][0].shape).toEqual([]);
  });

  test("a rule the server cannot read cannot be saved", async () => {
    reading = {
      startFrom: "2026-11-01",
      granularity: "",
      description: "",
      problems: ["FREQ=SOMETIMES is not a frequency"],
    };
    renderEditor();
    await settle();
    expect(screen.getByText("FREQ=SOMETIMES is not a frequency")).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Add override",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});

// The card as a whole, against a server that holds three Overrides and answers
// a reorder with them in the order it was sent.
describe("OverridesCard reordering", () => {
  function saved(id: string, description: string): Override {
    return {
      id,
      number: 0,
      rrule: "FREQ=WEEKLY;BYDAY=SU",
      startFrom: "2026-11-01",
      granularity: "date",
      description,
      problems: [],
      shape: null,
      pins: [],
    };
  }
  const STORED = [
    saved("o-a", "First"),
    saved("o-b", "Second"),
    saved("o-c", "Third"),
  ];
  const numbered = (ids: string[]) =>
    ids.map((id, i) => ({
      ...STORED.find((o) => o.id === id)!,
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
    if (url.endsWith("/overrides/order")) {
      const { ids } = JSON.parse(String(init?.body)) as { ids: string[] };
      sent.push(ids);
      return json({ overrides: numbered(ids) });
    }
    return json({ overrides: numbered(STORED.map((o) => o.id)) });
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
      Array.from(document.querySelectorAll("ol.overrides > li"));
    const realBox = HTMLElement.prototype.getBoundingClientRect;
    HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
      const i = items().indexOf(this as HTMLLIElement);
      return { top: Math.max(i, 0) * 100, height: 100 } as DOMRect;
    };
    const realCapture = HTMLElement.prototype.setPointerCapture;
    const capture = mock(() => {});
    HTMLElement.prototype.setPointerCapture = capture;
    try {
      const list = document.querySelector("ol.overrides")!;
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
