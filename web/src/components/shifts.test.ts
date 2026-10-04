import { describe, expect, test } from "bun:test";
import type { ShapeSeat } from "../types";
import { groupByRole, roleGroupLabel, shiftDeficit } from "./shifts";

// Deliberately not the Role names any deployment ships with: nothing here may
// match on a name, and a fixture named like this is what catches it if it
// starts to (issue #212).
const SHAPE: ShapeSeat[] = [
  { roleId: "r-duty", role: "Duty lead", count: 1 },
  { roleId: "r-hot", role: "Hot food", count: 2 },
  { roleId: "r-greet", role: "Greeter", count: 4 },
];

// Each fixture Role's id. A person carries both, like an Assignee: the id is
// what they are matched to a Seat by, the name what their group is headed with.
const ROLE_IDS: Record<string, string> = {
  "Duty lead": "r-duty",
  "Hot food": "r-hot",
  Greeter: "r-greet",
  "Washer up": "r-wash",
  "Litter picker": "r-litter",
  "": "",
};

function person(name: string, role: string) {
  return { name, roleId: ROLE_IDS[role], role };
}

describe("groupByRole", () => {
  test("groups people under the role they hold on this shift", () => {
    const grouped = groupByRole(SHAPE, [
      person("Alice", "Greeter"),
      person("Bob", "Duty lead"),
      person("Carol", "Greeter"),
    ]);

    expect(grouped).toEqual([
      {
        roleId: "r-duty",
        role: "Duty lead",
        people: [person("Bob", "Duty lead")],
      },
      {
        roleId: "r-greet",
        role: "Greeter",
        people: [person("Alice", "Greeter"), person("Carol", "Greeter")],
      },
    ]);
  });

  test("keeps the shape's order, which is the order the seats are filled", () => {
    const grouped = groupByRole(SHAPE, [
      person("Alice", "Greeter"),
      person("Bob", "Hot food"),
      person("Carol", "Duty lead"),
    ]);

    expect(grouped.map((g) => g.role)).toEqual([
      "Duty lead",
      "Hot food",
      "Greeter",
    ]);
  });

  test("a role the shape asks for but nobody holds is left out", () => {
    const grouped = groupByRole(SHAPE, [person("Alice", "Greeter")]);

    expect(grouped.map((g) => g.role)).toEqual(["Greeter"]);
  });

  test("keeps the order people are given in within a role", () => {
    const grouped = groupByRole(SHAPE, [
      person("Carol", "Hot food"),
      person("Alice", "Hot food"),
      person("Bob", "Hot food"),
    ]);

    expect(grouped[0].people.map((p) => p.name)).toEqual([
      "Carol",
      "Alice",
      "Bob",
    ]);
  });

  // An alteration records what happened on the day and is not held to the
  // Shape (ADR 0009), so somebody can genuinely hold a Role this shift never
  // asked for. Dropping them would take a name off the rota.
  test("a role outside the shape still gets a group, after the ones in it", () => {
    const grouped = groupByRole(SHAPE, [
      person("Alice", "Washer up"),
      person("Bob", "Greeter"),
    ]);

    expect(grouped).toEqual([
      {
        roleId: "r-greet",
        role: "Greeter",
        people: [person("Bob", "Greeter")],
      },
      {
        roleId: "r-wash",
        role: "Washer up",
        people: [person("Alice", "Washer up")],
      },
    ]);
  });

  test("roles outside the shape keep the order they first appear in", () => {
    const grouped = groupByRole(
      [],
      [
        person("Alice", "Washer up"),
        person("Bob", "Greeter"),
        person("Carol", "Washer up"),
      ],
    );

    expect(grouped.map((g) => g.role)).toEqual(["Washer up", "Greeter"]);
  });

  // An allocation predating the role column records no Role at all. It has
  // nothing to say rather than something to suppress, so it keeps the empty
  // Role it was given and goes last, behind every role somebody actually holds.
  test("people whose role was never recorded are grouped last", () => {
    const grouped = groupByRole(SHAPE, [
      person("Alice", ""),
      person("Bob", "Greeter"),
    ]);

    expect(grouped).toEqual([
      {
        roleId: "r-greet",
        role: "Greeter",
        people: [person("Bob", "Greeter")],
      },
      { roleId: "", role: "", people: [person("Alice", "")] },
    ]);
  });

  // Pins may go past a Shape (ADR 0010), so a Role can hold more people than
  // it has Seats. Every one of them is still on the rota.
  test("a role holding more people than its seats keeps them all", () => {
    const grouped = groupByRole(SHAPE, [
      person("Alice", "Duty lead"),
      person("Bob", "Duty lead"),
    ]);

    expect(grouped).toEqual([
      {
        roleId: "r-duty",
        role: "Duty lead",
        people: [person("Alice", "Duty lead"), person("Bob", "Duty lead")],
      },
    ]);
  });

  // The Shape and the people on a shift are matched by the Role's id, never
  // its name (issue #222). A person whose Role arrives under another name —
  // read before a rename the Shape has already caught up with — is still in
  // that Role's Seat, and the group is headed as the Shape names it.
  test("people are matched to the shape by role id, not by name", () => {
    const grouped = groupByRole(SHAPE, [
      { name: "Alice", roleId: "r-duty", role: "Shift lead" },
    ]);

    expect(grouped).toEqual([
      {
        roleId: "r-duty",
        role: "Duty lead",
        people: [{ name: "Alice", roleId: "r-duty", role: "Shift lead" }],
      },
    ]);
  });

  test("nobody on the shift is no groups at all", () => {
    expect(groupByRole(SHAPE, [])).toEqual([]);
  });
});

describe("roleGroupLabel", () => {
  test("a role is named as the server names it", () => {
    expect(roleGroupLabel("Hot food")).toBe("Hot food");
  });

  // Not a Role name invented for the occasion (ADR 0009): it is what to call a
  // group of people the rota records no Role for at all.
  test("a group with no role says so rather than going unlabelled", () => {
    expect(roleGroupLabel("")).toBe("Role not recorded");
  });
});

describe("shiftDeficit", () => {
  test("a role short of its seats says by how many", () => {
    expect(
      shiftDeficit(SHAPE, [
        person("Alice", "Duty lead"),
        person("Bob", "Hot food"),
      ]),
    ).toEqual([
      { role: "Hot food", deficit: 1 },
      { role: "Greeter", deficit: 4 },
    ]);
  });

  // A Seat is filled by whoever is in its Role, matched by id (issue #222):
  // somebody arriving under a name the Shape no longer uses still fills it.
  test("people fill the seats of their role by id, not by name", () => {
    const renamed = { roleId: "r-duty", role: "Shift lead" };
    expect(
      shiftDeficit(SHAPE, [
        renamed,
        person("Bob", "Hot food"),
        person("Carol", "Hot food"),
      ]),
    ).toEqual([{ role: "Greeter", deficit: 4 }]);
  });

  // Pins past the Shape (ADR 0010) fill their own Role and no other: one
  // Role overfilled is not a gap filled somewhere else.
  test("a role past its seats does not hide another role's gap", () => {
    const people = [
      person("Alice", "Duty lead"),
      person("Bob", "Duty lead"),
      person("Carol", "Hot food"),
      person("Dan", "Hot food"),
      person("Eve", "Litter picker"),
      ...["F", "G", "H"].map((n) => person(n, "Greeter")),
    ];

    expect(shiftDeficit(SHAPE, people)).toEqual([
      { role: "Greeter", deficit: 1 },
    ]);
  });
});
