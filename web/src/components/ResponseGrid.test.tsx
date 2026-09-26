import { describe, expect, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import ResponseGrid from "./ResponseGrid";
import type {
  AvailabilityEntry,
  AvailabilityGroup,
  AvailabilityRound,
} from "../types";

function member(id: string, name: string): AvailabilityEntry {
  return {
    volunteerId: id,
    volunteerName: name,
    link: `https://example.test/availability/${id}`,
    sentAt: null,
    replied: false,
    submittedAt: null,
    availableShiftIds: [],
    coveredBy: [],
    roles: [],
  };
}

function group(members: AvailabilityEntry[]): AvailabilityGroup {
  return {
    key: members.map((m) => m.volunteerId).join("+"),
    name: members.map((m) => m.volunteerName).join(" & "),
    replied: false,
    availableShiftIds: [],
    members,
  };
}

function round(
  groups: AvailabilityGroup[],
  allocated = false,
): AvailabilityRound {
  return {
    rotaId: "rota",
    start: "2026-10-04",
    end: "2026-10-25",
    allocated,
    shifts: [],
    groups,
  };
}

// Issue #226: a group's row named its members but linked none of them, so an
// Organiser answering for one of a pair had to open the row to find their form.
describe("ResponseGrid group names", () => {
  test("each member of a group links to their own form from the row", () => {
    const ann = member("ann", "Ann Lee");
    const bob = member("bob", "Bob Ray");
    render(
      <ResponseGrid round={round([group([ann, bob])])} onResend={() => {}} />,
    );

    expect(
      screen.getByRole("link", { name: "Ann Lee" }).getAttribute("href"),
    ).toBe(ann.link);
    expect(
      screen.getByRole("link", { name: "Bob Ray" }).getAttribute("href"),
    ).toBe(bob.link);
    expect(screen.getByRole("rowheader").textContent).toContain(
      "Ann Lee & Bob Ray",
    );
  });

  test("an allocated round links nobody", () => {
    const ann = member("ann", "Ann Lee");
    const bob = member("bob", "Bob Ray");
    render(
      <ResponseGrid
        round={round([group([ann, bob])], true)}
        onResend={() => {}}
      />,
    );

    expect(screen.queryAllByRole("link")).toHaveLength(0);
  });
});
