import { describe, expect, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import ShapeForm from "./ShapeForm";
import type { ConfiguredRole } from "../types";

// Named like no deployment's Roles, for the reason RotaEditDialogs.test.tsx
// gives (issue #212).
const ROLES: ConfiguredRole[] = [
  { id: "r-duty", name: "Duty lead", priority: 0, colour: "violet" },
  { id: "r-greet", name: "Greeter", priority: 1, colour: "teal" },
];

function renderForm() {
  render(
    <ShapeForm
      title="Default shape"
      intro="How many of each."
      saveLabel="Save shape"
      roles={ROLES}
      shape={[{ roleId: "r-duty", role: "Duty lead", count: 1 }]}
      onSave={async () => {}}
      onClose={() => {}}
    />,
  );
  return screen.getByRole("button", { name: "Save shape" });
}

describe("ShapeForm", () => {
  // Saving the Shape that is already there would change nothing, and on the
  // Allocation tab would still force a re-solve (issue #149).
  test("cannot be saved until a count is changed", () => {
    const save = renderForm();
    expect(save).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Greeter"), {
      target: { value: "2" },
    });
    expect(save).toBeEnabled();
  });

  // Nought is how a Role is left out, so a box cleared or put back to nought
  // asks for the same Shape as one never touched.
  test("a count put back to what it was is no change", () => {
    const save = renderForm();
    const greeter = screen.getByLabelText("Greeter");

    fireEvent.change(greeter, { target: { value: "3" } });
    fireEvent.change(greeter, { target: { value: "" } });
    expect(save).toBeDisabled();
  });
});
