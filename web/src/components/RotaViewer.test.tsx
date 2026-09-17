import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type { ConfiguredRole, RotaShift, Volunteer } from "../types";
import { SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";

// RotaViewer talks to the server through ../api exclusively (its hooks all
// funnel through it), so mounting it for real needs every function its
// hooks call stubbed out — there is no server to answer them here. Values
// only matter where a test below reads them back.
const fetchRoles = mock<() => Promise<ConfiguredRole[]>>();
const fetchVolunteers = mock<() => Promise<Volunteer[]>>();
const fetchDraftRotaAllocation = mock(async () => null);
const fetchPreallocations = mock(async () => []);

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
  { id: "lead", name: TEAM_LEAD_ROLE, priority: 0, colour: "violet" },
  {
    id: "vol",
    name: SERVICE_VOLUNTEER_ROLE,
    priority: 1,
    colour: "teal",
  },
];

const VOLUNTEERS: Volunteer[] = [
  {
    id: "alice",
    name: "Alice",
    fullName: "Alice",
    roles: [TEAM_LEAD_ROLE, SERVICE_VOLUNTEER_ROLE],
    group: null,
    gender: null,
    active: true,
  },
  {
    id: "carol",
    name: "Carol",
    fullName: "Carol",
    roles: [SERVICE_VOLUNTEER_ROLE],
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
          role: TEAM_LEAD_ROLE,
          custom: false,
          group: null,
          volunteerId: "alice",
        },
        {
          name: "Dan",
          role: SERVICE_VOLUNTEER_ROLE,
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
          role: SERVICE_VOLUNTEER_ROLE,
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
          role: SERVICE_VOLUNTEER_ROLE,
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
async function renderEditing() {
  render(
    <RotaViewer
      rotaShifts={shifts()}
      isAdmin
      onChange={mock(async () => {})}
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

// Alice is deliberately on two shifts (A and C, see shifts() above), so any
// query for "her chip" has to say which row — a plain screen.getByRole would
// find both and fail on ambiguity.
function rowFor(dateLabel: string): HTMLElement {
  const row = screen.getByText(dateLabel).closest(".shift-row");
  if (!row) throw new Error(`No .shift-row for ${dateLabel}`);
  return row as HTMLElement;
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
