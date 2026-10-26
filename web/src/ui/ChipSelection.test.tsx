import { describe, expect, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import Chip from "./Chip";
import { ChipSelectionProvider } from "./ChipSelection";

// Two chips that read the same but are different things, and a second copy of
// one of them.
function renderChips() {
  render(
    <ChipSelectionProvider>
      <Chip label="Sam" selectionKey="a" />
      <Chip label="Sam" selectionKey="b" />
      <Chip label="Sam" selectionKey="a" />
      <Chip label="Jo" selectionKey="c" />
    </ChipSelectionProvider>,
  );
  return screen.getAllByRole("button");
}

function pressed(buttons: HTMLElement[]): boolean[] {
  return buttons.map((b) => b.getAttribute("aria-pressed") === "true");
}

describe("ChipSelection", () => {
  test("selecting a chip highlights every chip with its key, and no other", () => {
    const buttons = renderChips();
    fireEvent.click(buttons[0]);
    expect(pressed(buttons)).toEqual([true, false, true, false]);
    expect(buttons[2].parentElement).toHaveClass("selected");
    expect(buttons[1].parentElement).not.toHaveClass("selected");
  });

  test("selecting it again clears it, and selecting another moves it", () => {
    const buttons = renderChips();
    fireEvent.click(buttons[0]);
    fireEvent.click(buttons[2]);
    expect(pressed(buttons)).toEqual([false, false, false, false]);
    fireEvent.click(buttons[0]);
    fireEvent.click(buttons[3]);
    expect(pressed(buttons)).toEqual([false, false, false, true]);
  });

  test("a chip with no key is only read", () => {
    render(
      <ChipSelectionProvider>
        <Chip label="Sam" />
      </ChipSelectionProvider>,
    );
    expect(screen.queryByRole("button")).toBeNull();
  });
});
