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

describe("ResponseGrid row order", () => {
  test("most availability first, then replied, no reply, not sent, then by name", () => {
    const shift = (id: string, closed = false) => ({
      id,
      date: "2026-10-04",
      start: "2026-10-04T19:30:00",
      end: "2026-10-04T21:30:00",
      closed,
      roles: [],
    });
    const sent = (m: AvailabilityEntry) => ({ ...m, sentAt: "2026-09-01" });
    const answered = (
      g: AvailabilityGroup,
      ids: string[],
    ): AvailabilityGroup => ({ ...g, replied: true, availableShiftIds: ids });

    const groups = [
      group([member("zed", "Zed Unsent")]),
      group([sent(member("yan", "Yan Silent"))]),
      answered(group([sent(member("xia", "Xia None"))]), []),
      answered(group([sent(member("cat", "Cat One"))]), ["a"]),
      // A closed shift's tick is not counted.
      answered(group([sent(member("bea", "Bea One"))]), ["b", "c"]),
      answered(group([sent(member("dan", "Dan Two"))]), ["a", "b"]),
      group([member("abe", "Abe Unsent")]),
    ];
    const r = {
      ...round(groups),
      shifts: [shift("a"), shift("b"), shift("c", true)],
    };
    render(<ResponseGrid round={r} onResend={() => {}} />);

    const names = screen
      .getAllByRole("rowheader")
      .map((h) => h.querySelector(".grid-name")?.textContent)
      .filter((n): n is string => n != null);
    expect(names).toEqual([
      "Dan Two",
      "Bea One",
      "Cat One",
      "Xia None",
      "Yan Silent",
      "Abe Unsent",
      "Zed Unsent",
    ]);
  });
});

// Issue #149: a send stamps each volunteer as their email goes, so a resend
// started while one runs would mail somebody twice.
describe("ResponseGrid resend", () => {
  test("is disabled while a send is running", async () => {
    const ann = member("ann", "Ann Lee");
    const { rerender } = render(
      <ResponseGrid round={round([group([ann])])} onResend={() => {}} />,
    );
    screen.getByRole("button", { name: "Links for Ann Lee" }).click();
    const send = await screen.findByRole("button", { name: "Send" });
    expect(send.hasAttribute("disabled")).toBe(false);

    rerender(
      <ResponseGrid
        round={round([group([ann])])}
        sending
        onResend={() => {}}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Send" }).hasAttribute("disabled"),
    ).toBe(true);
  });
});

describe("ResponseGrid columns", () => {
  test("two shifts on one day are headed by when each starts", () => {
    const shift = (id: string, start: string) => ({
      id,
      date: "2026-10-04",
      start: `2026-10-04T${start}:00`,
      end: "2026-10-04T23:00:00",
      closed: false,
      roles: [],
    });
    const r = {
      ...round([]),
      shifts: [shift("a", "10:00"), shift("b", "19:30")],
    };
    render(<ResponseGrid round={r} onResend={() => {}} />);

    const headers = screen
      .getAllByRole("columnheader")
      .map((h) => h.querySelector(".grid-hidden")?.textContent)
      .filter((n): n is string => n != null);
    expect(headers).toEqual([
      "Sunday 4 October at 10:00",
      "Sunday 4 October at 19:30",
    ]);
  });
});
