import { describe, expect, test } from "bun:test";
import { wholeShares } from "./wholeShares";

describe("wholeShares", () => {
  test("rounds each share and still makes the whole", () => {
    // A third each would round to 33 three times, and 99%.
    expect(wholeShares([1, 1, 1], 3)).toEqual([34, 33, 33]);
    expect(wholeShares([2, 1, 0], 3)).toEqual([67, 33, 0]);
  });

  test("gives a lost point to the share that lost most", () => {
    // 14.29, 28.57, 57.14: the second has the largest remainder.
    expect(wholeShares([1, 2, 4], 7)).toEqual([14, 29, 57]);
  });

  test("leaves exact shares alone", () => {
    expect(wholeShares([3, 1, 0], 4)).toEqual([75, 25, 0]);
  });
});
