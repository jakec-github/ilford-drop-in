import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type {
  ConfiguredRole,
  Preallocation,
  RotaChange,
  RotaShift,
  Volunteer,
} from "../types";
import type { AccessLevel } from "../auth-context";

// Deliberately not the Role names any deployment ships with. The rota page used
// to string-match on the two S1 came with, so nothing a deployment configured
// was reachable (issue #212); fixtures named like this are what stops that
// coming back.
const DUTY_LEAD = "Duty lead";
const HOT_FOOD = "Hot food";
const GREETER = "Greeter";

// RotaViewer talks to the server through ../api exclusively (its hooks all
// funnel through it), so mounting it for real needs every function its
// hooks call stubbed out — there is no server to answer them here. Values
// only matter where a test below reads them back.
const fetchRoles = mock<() => Promise<ConfiguredRole[]>>();
const fetchVolunteers = mock<() => Promise<Volunteer[]>>();
const fetchDraftRotaAllocation = mock(async () => null);
const fetchPreallocations = mock<() => Promise<Preallocation[]>>();

mock.module("../api", () => ({
  fetchRoles,
  createRole: mock(async () => {}),
  updateRole: mock(async () => {}),
  fetchVolunteers,
  syncVolunteers: mock(async () => {}),
  fetchDraftRotaAllocation,
  solveDraftRotaAllocation: mock(async () => {}),
  allocateRotaInFlight: mock(async () => ({
    outcome: "allocated",
    allocatedAt: "2026-01-01T00:00:00Z",
  })),
  fetchPreallocations,
  createPreallocation: mock(async () => {}),
  deletePreallocation: mock(async () => {}),
}));

const { default: RotaViewer } = await import("./RotaViewer");

afterAll(() => {
  mock.restore();
});

const ROLES: ConfiguredRole[] = [
  { id: "r-duty", name: DUTY_LEAD, priority: 0, colour: "violet" },
  { id: "r-hot", name: HOT_FOOD, priority: 1, colour: "amber" },
  { id: "r-greet", name: GREETER, priority: 2, colour: "teal" },
];

const VOLUNTEERS: Volunteer[] = [
  {
    id: "alice",
    name: "Alice",
    fullName: "Alice",
    roles: [DUTY_LEAD, GREETER],
    group: null,
    gender: null,
    active: true,
  },
  {
    id: "carol",
    name: "Carol",
    fullName: "Carol",
    roles: [GREETER],
    group: null,
    gender: null,
    active: true,
  },
];

// Three allocated shifts. Shift A is where Alice starts and gets picked up
// from — never a valid destination for her own move. Shift B is a clean,
// eligible destination. Shift C also has Alice already on it (issue #146's
// canReceive rule: nobody can be moved onto a shift they are already on),
// which is what test 2 needs a genuine, realistic destination to rule out.
function shifts(): RotaShift[] {
  return [
    {
      id: "shift-a",
      date: "2026-01-04",
      start: "2026-01-04T19:30:00",
      end: "2026-01-04T21:30:00",
      closed: false,
      allocated: true,
      shape: [],
      assignees: [
        {
          name: "Alice",
          roleId: "r-duty",
          role: DUTY_LEAD,
          custom: false,
          group: null,
          volunteerId: "alice",
        },
        {
          name: "Dan",
          roleId: "r-greet",
          role: GREETER,
          custom: false,
          group: null,
          volunteerId: "dan",
        },
      ],
    },
    {
      id: "shift-b",
      date: "2026-01-11",
      start: "2026-01-11T19:30:00",
      end: "2026-01-11T21:30:00",
      closed: false,
      allocated: true,
      shape: [],
      assignees: [
        {
          name: "Carol",
          roleId: "r-greet",
          role: GREETER,
          custom: false,
          group: null,
          volunteerId: "carol",
        },
      ],
    },
    {
      id: "shift-c",
      date: "2026-01-18",
      start: "2026-01-18T19:30:00",
      end: "2026-01-18T21:30:00",
      closed: false,
      allocated: true,
      shape: [],
      assignees: [
        {
          name: "Alice",
          roleId: "r-greet",
          role: GREETER,
          custom: false,
          group: null,
          volunteerId: "alice",
        },
      ],
    },
  ];
}

// Mounting kicks off four hook-driven fetches (roles, volunteers, the draft,
// preallocations) that resolve a tick later than render() itself returns —
// flushing them here, before a test's own synchronous clicks, is what keeps
// React from settling them mid-assertion with an unwrapped "not wrapped in
// act(...)" warning.
async function renderEditing(
  level: AccessLevel = "organiser",
  rotaShifts: RotaShift[] = shifts(),
  onChange: (change: RotaChange) => Promise<void> = mock(async () => {}),
) {
  render(
    <RotaViewer
      rotaShifts={rotaShifts}
      level={level}
      onChange={onChange}
      onSetClosed={mock(async () => {})}
      onSetTimes={mock(async () => {})}
      onSetShape={mock(async () => {})}
    />,
  );
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Edit rota" }));
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

// The rota as it is read rather than edited: no "Edit rota", so the chips are
// plain names and the rows carry only what everybody sees. Level still matters —
// the public is not shown the rota in flight at all.
async function renderRota(
  level: AccessLevel | null = null,
  rotaShifts: RotaShift[] = shifts(),
) {
  render(
    <RotaViewer
      rotaShifts={rotaShifts}
      level={level}
      onChange={mock(async () => {})}
      onSetClosed={mock(async () => {})}
      onSetTimes={mock(async () => {})}
      onSetShape={mock(async () => {})}
    />,
  );
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

// Alice is deliberately on two shifts (A and C, see shifts() above), so any
// query for "her chip" has to say which row — a plain screen.getByRole would
// find both and fail on ambiguity.
function rowFor(dateLabel: string): HTMLElement {
  const row = screen.getByText(dateLabel).closest(".shift-row");
  if (!row) throw new Error(`No .shift-row for ${dateLabel}`);
  return row as HTMLElement;
}

// One Role's block inside an expanded row, found by the name it is headed with —
// which is the whole point of the expanded view, so finding it any other way
// would test something else.
function groupFor(row: HTMLElement, roleLabel: string): HTMLElement {
  const heading = within(row).getByText(roleLabel);
  const group = heading.closest(".shift-role-group");
  if (!group) throw new Error(`No .shift-role-group for ${roleLabel}`);
  return group as HTMLElement;
}

// Picks Alice up from shift A via the tap route — the keyboard/touch
// equivalent of starting a drag, and the one usable from a test with no real
// pointer. Both routes call the same RotaViewer state, so this exercises
// exactly the eligibility logic a drag would.
function pickUpAlice() {
  fireEvent.click(
    within(rowFor("4 Jan")).getByRole("button", {
      name: "Alice, change this shift",
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Move or swap" }));
}

describe("RotaViewer placement", () => {
  beforeEach(() => {
    fetchRoles.mockClear();
    fetchRoles.mockResolvedValue(ROLES);
    fetchVolunteers.mockClear();
    fetchVolunteers.mockResolvedValue(VOLUNTEERS);
    fetchDraftRotaAllocation.mockClear();
    fetchDraftRotaAllocation.mockResolvedValue(null);
    fetchPreallocations.mockClear();
    fetchPreallocations.mockResolvedValue([]);
  });

  // A switch never takes anybody anywhere, so it has no drag equivalent and the
  // chip menu is its only route. It posts one change naming the same person on
  // both sides, which is what tells the server the Seat stays filled (#147).
  test("changing a role sends the same person in and out on the shift they are on", async () => {
    const onChange = mock(async () => {});
    await renderEditing("organiser", shifts(), onChange);

    fireEvent.click(
      within(rowFor("4 Jan")).getByRole("button", {
        name: "Alice, change this shift",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Change role" }));

    expect(
      await screen.findByRole("heading", { name: "Change Alice's role?" }),
    ).toBeInTheDocument();
    // Defaulted to the job she is doing there, not to the first Role going.
    expect((screen.getByLabelText("Role") as HTMLSelectElement).value).toBe(
      DUTY_LEAD,
    );

    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: HOT_FOOD },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Change role" }));
    });

    expect(onChange).toHaveBeenCalledWith({
      date: "2026-01-04",
      in: { volunteerId: "alice" },
      out: { volunteerId: "alice" },
      roleId: "r-hot",
      reason: "",
    });
  });

  test("swapping onto another person never offers a role field — the role is inherited, not chosen", async () => {
    await renderEditing();
    pickUpAlice();

    fireEvent.click(
      await screen.findByRole("button", { name: "Swap Alice with Carol" }),
    );

    expect(
      await screen.findByRole("heading", { name: "Swap Alice and Carol?" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();
  });

  test("the shift someone was picked up from is never offered back as a destination", async () => {
    await renderEditing();
    pickUpAlice();

    const shiftARow = screen.getByText("4 Jan").closest(".shift-row");
    expect(shiftARow).not.toBeNull();
    expect(
      within(shiftARow as HTMLElement).queryByRole("button", {
        name: "Move Alice here",
      }),
    ).not.toBeInTheDocument();
  });

  test("a shift the carried person is already on is not offered as a destination either", async () => {
    await renderEditing();
    pickUpAlice();

    // Shift B: clean destination, offered.
    const shiftBRow = screen.getByText("11 Jan").closest(".shift-row");
    expect(
      within(shiftBRow as HTMLElement).getByRole("button", {
        name: "Move Alice here",
      }),
    ).toBeInTheDocument();

    // Shift C: Alice is already on it, so moving her there would put her on
    // one shift twice — not offered, same as her own source row.
    const shiftCRow = screen.getByText("18 Jan").closest(".shift-row");
    expect(
      within(shiftCRow as HTMLElement).queryByRole("button", {
        name: "Move Alice here",
      }),
    ).not.toBeInTheDocument();
  });

  test("Escape cancels an in-flight pick, taking every destination affordance with it", async () => {
    await renderEditing();
    pickUpAlice();

    expect(await screen.findByText(/Carrying/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Move Alice here" }),
    ).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });

    expect(screen.queryByText(/Carrying/)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Move Alice here" }),
    ).not.toBeInTheDocument();
  });

  test("ending a real drag (dragend) clears the pick the same way Escape does", async () => {
    await renderEditing();

    // The drag route, not the tap route this time: dragstart on the chip
    // itself starts the pick with dragging: true, same as a real browser
    // drag would (RotaViewer.pickUp's third argument).
    const aliceOnShiftA = within(rowFor("4 Jan")).getByRole("button", {
      name: "Alice, change this shift",
    });
    // The pick itself is deferred a macrotask past dragstart (ShiftList.tsx's
    // Chip — the fix for issue #146's actual bug, where updating state
    // synchronously inside the native handler let Chromium cancel the drag
    // that had just started), so it needs a real tick before React sees it.
    const dataTransfer = { setData: () => {}, effectAllowed: "" };
    await act(async () => {
      fireEvent.dragStart(aliceOnShiftA, { dataTransfer });
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // Mid-drag, not tap mode, so the "Move here" button is deliberately
    // absent (it would shift the rows out from under the pointer); the
    // outline marking a valid destination is the drag-only equivalent.
    expect(
      within(rowFor("11 Jan")).getByRole("button", {
        name: "Swap Alice with Carol",
      }),
    ).toBeInTheDocument();
    expect(rowFor("11 Jan").className).toContain("drop-target");

    fireEvent.dragEnd(aliceOnShiftA);

    expect(rowFor("11 Jan").className).not.toContain("drop-target");
  });
});

// One shift of the rota in flight, beside shifts()'s allocated ones: it is where
// pins, closures and Shapes are offered, so it is where the two levels differ.
// Its Shape is what a pin is held to.
function withUnallocated(shape: RotaShift["shape"] = []): RotaShift[] {
  return [
    ...shifts(),
    {
      id: "shift-d",
      date: "2026-01-25",
      start: "2026-01-25T19:30:00",
      end: "2026-01-25T21:30:00",
      closed: false,
      allocated: false,
      shape,
      assignees: [],
    },
  ];
}

// A shift the drop-in does not run on: nobody is on it and nobody ever will be.
function withClosed(): RotaShift[] {
  return [
    ...shifts(),
    {
      id: "shift-e",
      date: "2026-01-25",
      start: "2026-01-25T19:30:00",
      end: "2026-01-25T21:30:00",
      closed: true,
      allocated: false,
      shape: [],
      assignees: [],
    },
  ];
}

// An allocation from before the rota recorded which Role it was made under.
function withUnrecordedRole(): RotaShift[] {
  const rota = shifts();
  rota[0].assignees = [
    ...rota[0].assignees,
    {
      name: "Erin",
      roleId: "",
      role: "",
      custom: false,
      group: null,
      volunteerId: "erin",
    },
  ];
  return rota;
}

// The two halves of ADR 0009, end to end through the page rather than through
// the dialogs alone: which Roles each picker actually gets handed.
describe("RotaViewer role pickers", () => {
  beforeEach(() => {
    fetchRoles.mockClear();
    fetchRoles.mockResolvedValue(ROLES);
    fetchVolunteers.mockClear();
    fetchVolunteers.mockResolvedValue(VOLUNTEERS);
    fetchDraftRotaAllocation.mockClear();
    fetchDraftRotaAllocation.mockResolvedValue(null);
    fetchPreallocations.mockClear();
    fetchPreallocations.mockResolvedValue([]);
  });

  // Adding to a published rota records what happened on the day, so the roster
  // and the Shape are advice: every configured Role is on offer.
  test("adding to an allocated shift offers every configured role", async () => {
    await renderEditing();

    // Carol is on 11 Jan already, so 4 Jan is where she can still be added.
    fireEvent.click(
      within(rowFor("4 Jan")).getByRole("button", { name: /^Add someone to/ }),
    );
    fireEvent.change(await screen.findByLabelText("Who"), {
      target: { value: "carol" },
    });

    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    expect(
      within(roleField)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual([DUTY_LEAD, HOT_FOOD, GREETER]);
    // Carol only greets on the roster, so the default follows her — but Duty
    // lead is hers to be given.
    expect(roleField.value).toBe(GREETER);
  });

  // Neither the roster nor this shift's Shape narrows a pin (ADR 0010): every
  // Role the drop-in has is on offer.
  test("pinning offers every role, held or not, in the shape or not", async () => {
    await renderEditing(
      "organiser",
      withUnallocated([
        { roleId: "r-duty", role: DUTY_LEAD, count: 1 },
        { roleId: "r-hot", role: HOT_FOOD, count: 2 },
      ]),
    );

    fireEvent.click(
      within(rowFor("25 Jan")).getByRole("button", {
        name: /^Pin someone to/,
      }),
    );
    fireEvent.change(await screen.findByLabelText("Who"), {
      target: { value: "alice" },
    });

    // Alice holds Duty lead and Greeter. Hot food is offered though she does
    // not hold it, and Greeter though this shift asks for none.
    expect(
      within(screen.getByLabelText("Role"))
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual([DUTY_LEAD, HOT_FOOD, GREETER]);
  });

  // Being inactive stops the allocator choosing somebody, not a person
  // deciding to pin them (ADR 0010).
  test("pinning offers inactive volunteers, marked as such", async () => {
    fetchVolunteers.mockResolvedValue([
      ...VOLUNTEERS,
      {
        id: "dora",
        name: "Dora",
        fullName: "Dora",
        roles: [GREETER],
        group: null,
        gender: null,
        active: false,
      },
    ]);
    await renderEditing("organiser", withUnallocated());

    fireEvent.click(
      within(rowFor("25 Jan")).getByRole("button", {
        name: /^Pin someone to/,
      }),
    );
    const who = await screen.findByLabelText("Who");

    expect(
      within(who).getByRole("option", { name: "Dora (not active)" }),
    ).toBeInTheDocument();
  });
});

describe("RotaViewer access levels", () => {
  beforeEach(() => {
    fetchRoles.mockClear();
    fetchRoles.mockResolvedValue(ROLES);
    fetchVolunteers.mockClear();
    fetchVolunteers.mockResolvedValue(VOLUNTEERS);
    fetchPreallocations.mockClear();
    fetchPreallocations.mockResolvedValue([]);
  });

  test("an Organiser can pin, close, reshape and retime a shift of the rota in flight", async () => {
    await renderEditing("organiser", withUnallocated());

    const row = rowFor("25 Jan");
    expect(
      within(row).getByRole("button", { name: /^Pin someone to/ }),
    ).toBeInTheDocument();
    expect(
      within(row).getByRole("button", { name: /^Close/ }),
    ).toBeInTheDocument();
    expect(
      within(row).getByRole("button", { name: /^Change what .* asks for$/ }),
    ).toBeInTheDocument();
    expect(
      within(row).getByRole("button", { name: /^Change when .* runs$/ }),
    ).toBeInTheDocument();
  });

  test("a Rota Editor can pin, but not close, reshape or retime", async () => {
    await renderEditing("rotaEditor", withUnallocated());

    const row = rowFor("25 Jan");
    expect(
      within(row).getByRole("button", { name: /^Pin someone to/ }),
    ).toBeInTheDocument();
    expect(
      within(row).queryByRole("button", { name: /^Close/ }),
    ).not.toBeInTheDocument();
    expect(
      within(row).queryByRole("button", { name: /asks for$/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Change when .* runs$/ }),
    ).not.toBeInTheDocument();
  });

  test("a Rota Editor can still change who is on an allocated shift", async () => {
    await renderEditing("rotaEditor");

    fireEvent.click(
      within(rowFor("4 Jan")).getByRole("button", {
        name: "Alice, change this shift",
      }),
    );
    expect(
      screen.getByRole("button", { name: "Move or swap" }),
    ).toBeInTheDocument();
  });

  test("a Rota Editor is not pointed at the Organiser area", async () => {
    await renderEditing("rotaEditor", withUnallocated());

    expect(screen.getByText(/rota in flight/)).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: /Organiser/ }),
    ).not.toBeInTheDocument();
  });

  test("an Organiser is pointed at the Allocation tab", async () => {
    await renderEditing("organiser", withUnallocated());

    expect(screen.getByRole("link", { name: /Organiser/ })).toHaveAttribute(
      "href",
      "/organiser/allocation",
    );
  });

  test("logged out, there is nothing to edit and the rota in flight is hidden", async () => {
    render(
      <RotaViewer
        rotaShifts={withUnallocated()}
        level={null}
        onChange={mock(async () => {})}
        onSetClosed={mock(async () => {})}
        onSetTimes={mock(async () => {})}
        onSetShape={mock(async () => {})}
      />,
    );
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(
      screen.queryByRole("button", { name: "Edit rota" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("25 Jan")).not.toBeInTheDocument();
  });
});

// A shift row's expanded view (issue #66). Role used to be encoded only as chip
// colour, which a reader has to already know the convention to decode and which
// says nothing at all to one who cannot see it. Expanding a row names each Role
// in text and gathers its people under it.
describe("RotaViewer expanded shift view", () => {
  beforeEach(() => {
    fetchRoles.mockClear();
    fetchRoles.mockResolvedValue(ROLES);
    fetchVolunteers.mockClear();
    fetchVolunteers.mockResolvedValue(VOLUNTEERS);
    fetchDraftRotaAllocation.mockClear();
    fetchDraftRotaAllocation.mockResolvedValue(null);
    fetchPreallocations.mockClear();
    fetchPreallocations.mockResolvedValue([]);
  });

  test("a row expands to name each role, and collapses again", async () => {
    await renderRota();

    // 4 Jan has Alice on Duty lead and Dan greeting.
    const row = rowFor("4 Jan");
    fireEvent.click(
      within(row).getByRole("button", { name: "Show details for Sun 4 Jan" }),
    );

    const duty = groupFor(row, DUTY_LEAD);
    expect(within(duty).getByText("Alice")).toBeInTheDocument();
    const greeters = groupFor(row, GREETER);
    expect(within(greeters).getByText("Dan")).toBeInTheDocument();

    fireEvent.click(
      within(row).getByRole("button", { name: "Hide details for Sun 4 Jan" }),
    );

    expect(within(row).queryByText(DUTY_LEAD)).not.toBeInTheDocument();
    // The names never went anywhere — collapsing puts them back in one strip.
    expect(within(row).getByText("Alice")).toBeInTheDocument();
  });

  test("a row that is expanded says so, and one that is not says that", async () => {
    await renderRota();

    const row = rowFor("4 Jan");
    const toggle = within(row).getByRole("button", {
      name: "Show details for Sun 4 Jan",
    });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(toggle);

    expect(
      within(row).getByRole("button", { name: "Hide details for Sun 4 Jan" }),
    ).toHaveAttribute("aria-expanded", "true");
  });

  test("only the row that was expanded expands", async () => {
    await renderRota();

    fireEvent.click(
      within(rowFor("4 Jan")).getByRole("button", {
        name: "Show details for Sun 4 Jan",
      }),
    );

    expect(
      within(rowFor("11 Jan")).queryByText(GREETER),
    ).not.toBeInTheDocument();
  });

  // The chips in an expanded row are the same chips, regrouped — not copies of
  // them — so everything a chip does still works.
  test("picking a name out of an expanded row still selects it across the rota", async () => {
    await renderRota();

    const row = rowFor("4 Jan");
    fireEvent.click(
      within(row).getByRole("button", { name: "Show details for Sun 4 Jan" }),
    );
    fireEvent.click(within(row).getByRole("button", { name: "Alice" }));

    expect(screen.getByText(/Upcoming:/)).toBeInTheDocument();
  });

  test("a closed shift has nothing to expand", async () => {
    await renderRota("organiser", withClosed());

    expect(
      within(rowFor("25 Jan")).queryByRole("button", {
        name: /details for/,
      }),
    ).not.toBeInTheDocument();
  });

  // A shift of the rota in flight has pins and drafted names rather than
  // assignees, and they group under their Roles the same way.
  test("a shift of the rota in flight groups who is expected on it by role", async () => {
    fetchPreallocations.mockResolvedValue([
      {
        id: "pin-1",
        date: "2026-01-25",
        roleId: "r-duty",
        role: DUTY_LEAD,
        name: "Alice",
        custom: false,
        volunteerId: "alice",
      },
    ]);
    await renderRota("organiser", withUnallocated());

    const row = rowFor("25 Jan");
    fireEvent.click(
      within(row).getByRole("button", { name: "Show details for Sun 25 Jan" }),
    );

    expect(
      within(groupFor(row, DUTY_LEAD)).getByText("Alice"),
    ).toBeInTheDocument();
  });

  // An allocation predating the role column records no Role at all. It is named
  // as such rather than left under a blank heading.
  test("somebody the rota records no role for is said to have none", async () => {
    await renderRota("organiser", withUnrecordedRole());

    const row = rowFor("4 Jan");
    fireEvent.click(
      within(row).getByRole("button", { name: "Show details for Sun 4 Jan" }),
    );

    expect(
      within(groupFor(row, "Role not recorded")).getByText("Erin"),
    ).toBeInTheDocument();
  });
});
