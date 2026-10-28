import { describe, expect, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import { NoShifts } from "./SetupNeeded";

describe("NoShifts", () => {
  test("a visitor who may be an Organiser is asked to log in", () => {
    render(<NoShifts level={null} />);
    expect(screen.getByText("No shifts yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "log in" })).toHaveAttribute(
      "href",
      expect.stringContaining("/auth/login"),
    );
  });

  test("a Rota Editor is told to wait, not pointed at Config", () => {
    render(<NoShifts level="rotaEditor" />);
    expect(screen.getByText("No shifts yet")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
