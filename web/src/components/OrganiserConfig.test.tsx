import { describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { NewRoleCard, RoleCard, type RoleLook } from "./OrganiserConfig";
import type { ConfiguredRole } from "../types";

// A Role named unlike any real Organisation's: the card draws whatever it is
// given.
const ROLE: ConfiguredRole = {
  id: "r-duty",
  name: "Duty lead",
  priority: 1,
  colour: "violet",
};

// Long enough for a change to stop moving and be saved.
const saveWait = () =>
  act(() => new Promise<void>((resolve) => setTimeout(resolve, 1000)));

function renderRole(holders: number | null = 3) {
  const onSave = mock<(look: RoleLook, renamed: boolean) => Promise<void>>(() =>
    Promise.resolve(),
  );
  render(
    <RoleCard
      role={ROLE}
      place={0}
      count={1}
      holders={holders}
      busy={false}
      held={false}
      onSave={onSave}
      onGrab={() => {}}
      onMove={() => {}}
    />,
  );
  return onSave;
}

describe("RoleCard", () => {
  test("says how many active volunteers hold the Role", () => {
    renderRole(3);
    expect(screen.getByText("3 volunteers")).toBeInTheDocument();
  });

  test("a rename saves itself and warns that the sheet must follow", async () => {
    const onSave = renderRole();
    fireEvent.change(screen.getByLabelText("Name of Duty lead"), {
      target: { value: "Shift lead " },
    });
    expect(screen.getByRole("status").textContent).toContain(
      "change “Duty lead” to “Shift lead” there too",
    );
    await saveWait();
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0]).toEqual([
      { name: "Shift lead ", colour: "violet" },
      true,
    ]);
  });

  test("a new colour saves itself, and is not a rename", async () => {
    const onSave = renderRole();
    fireEvent.click(screen.getByRole("radio", { name: "teal" }));
    await saveWait();
    expect(onSave.mock.calls[0]).toEqual([
      { name: "Duty lead", colour: "teal" },
      false,
    ]);
  });

  test("a blank name is not saved", async () => {
    const onSave = renderRole();
    fireEvent.change(screen.getByLabelText("Name of Duty lead"), {
      target: { value: "  " },
    });
    await saveWait();
    expect(screen.getByText("A Role needs a name.")).toBeInTheDocument();
    expect(onSave).not.toHaveBeenCalled();
  });
});

describe("NewRoleCard", () => {
  test("nothing is added until it has a name, then it is added trimmed", async () => {
    const onAdd = mock<(look: RoleLook) => Promise<void>>(() =>
      Promise.resolve(),
    );
    render(<NewRoleCard onAdd={onAdd} onDiscard={() => {}} />);
    fireEvent.click(screen.getByRole("radio", { name: "teal" }));
    await saveWait();
    expect(onAdd).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Name of the new role"), {
      target: { value: " Greeter " },
    });
    await saveWait();
    expect(onAdd.mock.calls[0]).toEqual([{ name: "Greeter", colour: "teal" }]);
  });
});
