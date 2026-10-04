import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen, within } from "@testing-library/react";
import StandingPreallocationForm from "./StandingPreallocationForm";
import type {
  ConfiguredRole,
  NewStandingPreallocation,
  Volunteer,
} from "../types";

// Named like no deployment's Roles, for the reason RotaEditDialogs.test.tsx
// gives (issue #212).
const DUTY_LEAD = "Duty lead";
const HOT_FOOD = "Hot food";
const GREETER = "Greeter";

// As the API lists them: highest priority first.
const ROLES: ConfiguredRole[] = [
  { id: "r-duty", name: DUTY_LEAD, priority: 0, colour: "violet" },
  { id: "r-hot", name: HOT_FOOD, priority: 1, colour: "amber" },
  { id: "r-greet", name: GREETER, priority: 2, colour: "teal" },
];

const grace: Volunteer = {
  id: "grace",
  name: "Grace",
  fullName: "Grace Hopper",
  roles: [GREETER],
  group: null,
  gender: null,
  active: true,
};

const ada: Volunteer = {
  id: "ada",
  name: "Ada",
  fullName: "Ada Lovelace",
  roles: [HOT_FOOD, GREETER],
  group: null,
  gender: null,
  active: true,
};

function renderForm(
  onSave = mock<(standing: NewStandingPreallocation) => Promise<void>>(
    async () => {},
  ),
) {
  render(
    <StandingPreallocationForm
      roles={ROLES}
      volunteers={[grace, ada]}
      volunteersError={null}
      onSave={onSave}
      onClose={() => {}}
    />,
  );
  return onSave;
}

function roleField(): HTMLSelectElement {
  return screen.getByLabelText("Role") as HTMLSelectElement;
}

describe("StandingPreallocationForm", () => {
  // Not holding a Role is a rule, and rules bind the allocator, not whoever is
  // pinning (ADR 0010).
  test("offers every configured role, not only the ones the volunteer holds", () => {
    renderForm();

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });

    expect(
      within(roleField())
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual([DUTY_LEAD, HOT_FOOD, GREETER]);
  });

  test("the role defaults to the highest-priority one the volunteer holds", () => {
    renderForm();

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });
    expect(roleField().value).toBe("r-hot");

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });
    expect(roleField().value).toBe("r-greet");
  });

  // A warning, never a block (ADR 0010).
  test("pinning into a role the volunteer does not hold says so, and is allowed", () => {
    const onSave = renderForm();

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });
    expect(screen.queryByText(/is not down for/)).not.toBeInTheDocument();

    fireEvent.change(roleField(), { target: { value: "r-duty" } });
    expect(
      screen.getByText(new RegExp(`Grace is not down for ${DUTY_LEAD}`)),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add pin" }));

    expect(onSave).toHaveBeenCalledWith({
      rrule: "FREQ=WEEKLY;BYDAY=SU",
      roleId: "r-duty",
      person: { volunteerId: "grace" },
    });
  });

  test("someone off the roster is offered every role, with no warning", () => {
    renderForm();

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Redbridge youth group" },
    });

    expect(roleField().value).toBe("r-duty");
    expect(screen.queryByText(/is not down for/)).not.toBeInTheDocument();
  });
});
