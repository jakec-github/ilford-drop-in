import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import Dialog from "./Dialog";

function renderDialog(dismissable?: boolean) {
  const onClose = mock(() => {});
  render(
    <Dialog title="Save this?" onClose={onClose} dismissable={dismissable}>
      <p>Body</p>
    </Dialog>,
  );
  return { onClose, dialog: screen.getByRole("dialog") };
}

// Escape fires cancel on a modal dialog, and cancel is what holding it off
// turns on.
function pressEscape(dialog: HTMLElement): boolean {
  const cancel = new Event("cancel", { cancelable: true });
  fireEvent(dialog, cancel);
  return cancel.defaultPrevented;
}

describe("Dialog", () => {
  test("closes on a backdrop click and lets Escape through by default", () => {
    const { onClose, dialog } = renderDialog();

    expect(pressEscape(dialog)).toBe(false);
    fireEvent.click(dialog);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  // While what the dialog set going is still out, neither way of shutting it
  // from outside works (issue #149).
  test("holds Escape and the backdrop off while it is not dismissable", () => {
    const { onClose, dialog } = renderDialog(false);

    expect(pressEscape(dialog)).toBe(true);
    fireEvent.click(dialog);
    expect(onClose).not.toHaveBeenCalled();
  });

  test("a click inside the dialog never closes it", () => {
    const { onClose } = renderDialog();

    fireEvent.click(screen.getByText("Body"));
    expect(onClose).not.toHaveBeenCalled();
  });
});
