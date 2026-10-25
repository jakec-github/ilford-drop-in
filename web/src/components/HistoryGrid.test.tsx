import { describe, expect, test } from "bun:test";
import { fireEvent, render, screen, within } from "@testing-library/react";
import HistoryGrid from "./HistoryGrid";
import type { HistoryEntry, VolunteerHistory } from "../types";

function entry(over: Partial<HistoryEntry>): HistoryEntry {
  return {
    availability: "ok",
    available: 3,
    openShifts: 8,
    given: 2,
    attended: 2,
    ...over,
  };
}

const history: VolunteerHistory = {
  rotations: [
    { id: "r1", start: "2026-01-04", end: "2026-02-22" },
    { id: "r2", start: "2026-03-01", end: "2026-04-19" },
  ],
  volunteers: [
    {
      id: "a",
      name: "Alice",
      fullName: "Alice Adams",
      active: true,
      history: [
        entry({ availability: "good", available: 6, attended: 3 }),
        entry({ availability: "good", available: 7, attended: 4 }),
      ],
    },
    {
      id: "b",
      name: "Bob",
      fullName: "Bob Barnes",
      active: true,
      history: [
        entry({ availability: "no_response", available: 0, attended: 0 }),
        entry({ availability: "low", available: 1, attended: 1 }),
      ],
    },
    {
      id: "c",
      name: "Cat",
      fullName: "Cat Cole",
      active: true,
      history: [
        entry({ availability: "not_sent", available: 0, attended: 0 }),
        entry({ availability: "ok", available: 3, attended: 5 }),
      ],
    },
  ],
};

function names(): string[] {
  return screen.getAllByRole("rowheader").map((cell) => cell.textContent ?? "");
}

describe("HistoryGrid", () => {
  test("shows each availability band in words", () => {
    render(<HistoryGrid history={history} />);
    const bob = screen.getByRole("row", { name: /Bob Barnes/ });
    expect(within(bob).getByText("Not responded")).toBeTruthy();
    expect(within(bob).getByText("Low")).toBeTruthy();
    const cat = screen.getByRole("row", { name: /Cat Cole/ });
    expect(within(cat).getByText("Not sent")).toBeTruthy();
  });

  test("switches to shifts attended", () => {
    render(<HistoryGrid history={history} />);
    fireEvent.click(screen.getByRole("radio", { name: "Shifts attended" }));
    const cat = screen.getByRole("row", { name: /Cat Cole/ });
    expect(within(cat).getByText("5")).toBeTruthy();
  });

  test("sorts by lowest availability, ignoring rotas never sent", () => {
    render(<HistoryGrid history={history} />);
    fireEvent.change(screen.getByLabelText("Sort by"), {
      target: { value: "availability" },
    });
    // Bob offered 1 of 16; Cat 3 of 8 (her unsent rota does not count); Alice 13 of 16.
    expect(names()).toEqual(["Bob Barnes", "Cat Cole", "Alice Adams"]);
  });

  test("sorts by lowest attendance", () => {
    render(<HistoryGrid history={history} />);
    fireEvent.change(screen.getByLabelText("Sort by"), {
      target: { value: "attendance" },
    });
    expect(names()).toEqual(["Bob Barnes", "Cat Cole", "Alice Adams"]);
  });
});
