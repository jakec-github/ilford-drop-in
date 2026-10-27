import { describe, expect, test } from "bun:test";
import {
  addDays,
  endOf,
  formatDay,
  formatLength,
  formatStart,
  isDate,
  monthsFrom,
} from "./patternDates";

describe("isDate", () => {
  test("takes a calendar date and nothing else", () => {
    expect(isDate("2026-11-01")).toBe(true);
    expect(isDate("2026-02-30")).toBe(false);
    expect(isDate("2026-11-1")).toBe(false);
    expect(isDate("")).toBe(false);
  });
});

describe("formatting", () => {
  test("says a day and a start with the weekday", () => {
    expect(formatDay("2026-11-01")).toBe("Sun 1 Nov 2026");
    expect(formatStart("2026-11-01T19:30:00")).toBe("Sun 1 Nov 2026 at 19:30");
  });

  test("says a length in hours and minutes", () => {
    expect(formatLength(120)).toBe("2 hours");
    expect(formatLength(45)).toBe("45 minutes");
    expect(formatLength(135)).toBe("2 hours 15 minutes");
    expect(formatLength(61)).toBe("1 hour 1 minute");
  });
});

describe("endOf", () => {
  test("adds the length to the wall-clock start", () => {
    expect(endOf("2026-11-01T19:30:00", 120)).toEqual({
      time: "21:30",
      nextDay: false,
    });
  });

  test("says when a shift runs past midnight", () => {
    expect(endOf("2026-11-01T22:00:00", 180)).toEqual({
      time: "01:00",
      nextDay: true,
    });
  });
});

describe("dates across the clocks changing", () => {
  // Every date is pinned to UTC, so the last Sunday of October is still one
  // day long whatever zone the browser is in.
  test("a day added is a calendar day", () => {
    expect(addDays("2026-10-24", 1)).toBe("2026-10-25");
    expect(addDays("2026-10-25", 1)).toBe("2026-10-26");
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
  });
});

describe("monthsFrom", () => {
  test("lays out the months Monday first", () => {
    const [nov, dec, jan] = monthsFrom("2026-11-15", 0, 3);
    expect(nov.label).toBe("November 2026");
    expect(nov.first).toBe("2026-11-01");
    expect(nov.next).toBe("2026-12-01");
    // 1 November 2026 is a Sunday: six blanks before it.
    expect(nov.lead).toBe(6);
    expect(nov.days).toHaveLength(30);
    expect(dec.key).toBe("2026-12");
    expect(jan.label).toBe("January 2027");
  });

  test("steps by the offset", () => {
    expect(monthsFrom("2026-11-15", -3, 1)[0].key).toBe("2026-08");
  });
});
