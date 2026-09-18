import { describe, expect, test } from "bun:test";
import type { ShapeSeat } from "../types";
import { describeShape, seatCounts } from "./shape";

// Deliberately not the Role names any deployment ships with: a picker that has
// gone back to matching on a name rather than on the Shape in front of it
// cannot pass a test whose Roles are named like this (issue #212).
const SHAPE: ShapeSeat[] = [
  { roleId: "r-duty", role: "Duty lead", count: 1 },
  { roleId: "r-hot", role: "Hot food", count: 2 },
  { roleId: "r-greet", role: "Greeter", count: 4 },
];

describe("describeShape", () => {
  test("reads as prose in the order the seats are filled", () => {
    expect(describeShape(SHAPE)).toBe("1 Duty lead, 2 Hot food, 4 Greeter");
  });
});

describe("seatCounts", () => {
  test("a shape nobody is pinned to has every seat free", () => {
    expect(seatCounts(SHAPE, [])).toEqual([
      { roleId: "r-duty", role: "Duty lead", seats: 1, taken: 0 },
      { roleId: "r-hot", role: "Hot food", seats: 2, taken: 0 },
      { roleId: "r-greet", role: "Greeter", seats: 4, taken: 0 },
    ]);
  });

  test("counts what is already taken against the role it was taken from", () => {
    const counted = seatCounts(SHAPE, ["r-hot", "r-greet", "r-hot"]);

    expect(counted.map((s) => s.taken)).toEqual([0, 2, 1]);
  });

  test("keeps the shape's order, which is the order the seats are filled", () => {
    expect(seatCounts(SHAPE, []).map((s) => s.role)).toEqual([
      "Duty lead",
      "Hot food",
      "Greeter",
    ]);
  });

  // A pin made before its Role was dropped from this shift's Shape, or before
  // the Shape was narrowed. It is still on the shift and still shown; what it
  // must not do is make some other Role look full.
  test("something taken in a role the shape does not ask for is ignored", () => {
    expect(seatCounts(SHAPE, ["r-retired"]).map((s) => s.taken)).toEqual([
      0, 0, 0,
    ]);
  });

  test("a shift asking for nobody has no seats of anything", () => {
    expect(seatCounts([], ["r-hot"])).toEqual([]);
  });
});
