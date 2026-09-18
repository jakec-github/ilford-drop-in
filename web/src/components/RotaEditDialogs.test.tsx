import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen, within } from "@testing-library/react";
import {
  AssigneeDialog,
  ConfirmChangeDialog,
  PinDialog,
} from "./RotaEditDialogs";
import type { Role, Volunteer } from "../types";
import { seatCounts } from "./shape";

// Every Role named in this file is deliberately one no deployment ships with.
// The pickers used to string-match on the two names S1 came with, which is what
// issue #212 is: a deployment that named its Roles anything else could not add,
// replace, move or pin anybody. Fixtures named like this are what stops that
// coming back — a regression to a hardcoded name cannot pass them.
const DUTY_LEAD = "Duty lead";
const HOT_FOOD = "Hot food";
const GREETER = "Greeter";

// As the API lists them: highest priority first.
const ROLES: Role[] = [DUTY_LEAD, HOT_FOOD, GREETER];

function optionsOf(field: HTMLElement): string[] {
  return within(field)
    .getAllByRole("option")
    .map((o) => o.textContent);
}

// A move: somebody leaves one shift and lands on another, so nothing is lost
// and the reason is the editor's to give or skip (issue #148).
function baseProps() {
  return {
    title: "Move Grace?",
    summary: "Grace moves from Sun 23 Aug to Sun 30 Aug.",
    confirmLabel: "Move",
    busy: false,
    reasonRequired: false,
    onCancel: () => {},
  };
}

// A remove: the shift comes out of it a pair of hands short, which is the one
// change that has to say why.
function removeProps() {
  return {
    title: "Remove Grace?",
    summary: "Grace comes off the shift on Sun 23 Aug.",
    confirmLabel: "Remove",
    busy: false,
    reasonRequired: true,
    onCancel: () => {},
  };
}

describe("ConfirmChangeDialog", () => {
  test("offers no role field for a remove or swap, where no role prop is passed", () => {
    render(<ConfirmChangeDialog {...baseProps()} onConfirm={() => {}} />);

    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();
  });

  test("a move offers a role choice, defaulted to what the volunteer was already doing", () => {
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: DUTY_LEAD, roles: ROLES }}
        onConfirm={() => {}}
      />,
    );

    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    expect(roleField.value).toBe(DUTY_LEAD);
  });

  // The Shape and the roster bind the allocator, not somebody recording what
  // happened on the day (ADR 0009), so the move picker offers the lot.
  test("every configured role is on offer, in priority order", () => {
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: GREETER, roles: ROLES }}
        onConfirm={() => {}}
      />,
    );

    expect(optionsOf(screen.getByLabelText("Role"))).toEqual([
      DUTY_LEAD,
      HOT_FOOD,
      GREETER,
    ]);
  });

  test("changing the role choice and confirming passes the chosen role, not the initial one", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: DUTY_LEAD, roles: ROLES }}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: HOT_FOOD },
    });
    fireEvent.change(screen.getByPlaceholderText("e.g. away that week"), {
      target: { value: "covering, not leading" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Move" }));

    expect(onConfirm).toHaveBeenCalledWith("covering, not leading", HOT_FOOD);
  });

  // A rota allocated before the Role was retired still names it. Dropping it
  // from the list would quietly move the person into whatever came first.
  test("a role the drop-in no longer offers is still listed, and stays selected", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: "Tea urn", roles: ROLES }}
        onConfirm={onConfirm}
      />,
    );

    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    expect(optionsOf(roleField)).toEqual(["Tea urn", ...ROLES]);
    expect(roleField.value).toBe("Tea urn");

    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(onConfirm).toHaveBeenCalledWith("", "Tea urn");
  });

  // An allocation predating the role column carries none, and api.ts no longer
  // invents one for it (issue #212). Moving that person must not silently give
  // them a Role they were never recorded in.
  test("someone the rota records no role for keeps none, and can be given one", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: "", roles: ROLES }}
        onConfirm={onConfirm}
      />,
    );

    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    expect(roleField.value).toBe("");
    expect(optionsOf(roleField)).toEqual(["No role", ...ROLES]);

    fireEvent.change(roleField, { target: { value: HOT_FOOD } });
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(onConfirm).toHaveBeenCalledWith("", HOT_FOOD);
  });

  test("a removal cannot be confirmed until a reason is entered", () => {
    render(<ConfirmChangeDialog {...removeProps()} onConfirm={() => {}} />);

    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("e.g. away that week"), {
      target: { value: "away that week" },
    });

    expect(screen.getByRole("button", { name: "Remove" })).toBeEnabled();
  });

  test("a change that is not a removal can be confirmed with no reason at all", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(<ConfirmChangeDialog {...baseProps()} onConfirm={onConfirm} />);

    const confirm = screen.getByRole("button", { name: "Move" });
    expect(confirm).toBeEnabled();

    fireEvent.click(confirm);

    expect(onConfirm).toHaveBeenCalledWith("", undefined);
  });

  test("the reason is marked optional only where it is", () => {
    const { unmount } = render(
      <ConfirmChangeDialog {...baseProps()} onConfirm={() => {}} />,
    );
    expect(screen.getByLabelText("Reason (optional)")).toBeInTheDocument();
    unmount();

    render(<ConfirmChangeDialog {...removeProps()} onConfirm={() => {}} />);
    expect(screen.getByLabelText("Reason")).toBeInTheDocument();
  });
});

// Grace does one job; Ada does two, the higher-priority one first as the API
// lists them.
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

function assigneeProps() {
  return {
    dateLabel: "Sun 23 Aug",
    volunteers: [grace, ada],
    volunteersError: null,
    roles: ROLES,
    busy: false,
    onCancel: () => {},
  };
}

function assignee(name: string, role: Role) {
  return { name, role, custom: false, group: null, volunteerId: name };
}

describe("AssigneeDialog", () => {
  // Adding somebody and replacing somebody both leave the shift no worse off,
  // so neither asks why (issue #148).
  test("an add needs only a person, not a reason", () => {
    const onConfirm =
      mock<(person: unknown, reason: string, role?: string) => void>();
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "add" }}
        onConfirm={onConfirm}
      />,
    );

    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(onConfirm).toHaveBeenCalledWith(
      { volunteerId: "grace" },
      "",
      GREETER,
    );
  });

  test("the role defaults to the highest-priority one the volunteer holds", () => {
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "add" }}
        onConfirm={() => {}}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });

    expect((screen.getByLabelText("Role") as HTMLSelectElement).value).toBe(
      HOT_FOOD,
    );
  });

  // The roster is standing advice, not a gate on a cover (ADR 0005/0009): who
  // is up to the job on the day is the call of whoever is changing the rota.
  test("every configured role is on offer, not only the ones the volunteer holds", () => {
    const onConfirm =
      mock<(person: unknown, reason: string, role?: string) => void>();
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "add" }}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });
    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    expect(optionsOf(roleField)).toEqual([DUTY_LEAD, HOT_FOOD, GREETER]);

    fireEvent.change(roleField, { target: { value: DUTY_LEAD } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(onConfirm).toHaveBeenCalledWith(
      { volunteerId: "grace" },
      "",
      DUTY_LEAD,
    );
  });

  test("a replacement hands the outgoing person's role over, and says so", () => {
    const onConfirm =
      mock<(person: unknown, reason: string, role?: string) => void>();
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "replace", outgoing: assignee("Dan", DUTY_LEAD) }}
        onConfirm={onConfirm}
      />,
    );

    expect(screen.getByText(/as duty lead/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });
    expect((screen.getByLabelText("Role") as HTMLSelectElement).value).toBe(
      DUTY_LEAD,
    );

    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    expect(onConfirm).toHaveBeenCalledWith(
      { volunteerId: "grace" },
      "",
      DUTY_LEAD,
    );
  });

  test("a replacement can take a different role from the person it replaces", () => {
    const onConfirm =
      mock<(person: unknown, reason: string, role?: string) => void>();
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "replace", outgoing: assignee("Dan", DUTY_LEAD) }}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });
    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: GREETER },
    });
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));

    expect(onConfirm).toHaveBeenCalledWith(
      { volunteerId: "grace" },
      "",
      GREETER,
    );
  });

  // The API refuses a volunteer coming in with no Role, so replacing somebody
  // the rota records none for has to settle one rather than pass nothing along.
  test("replacing someone with no recorded role still states one", () => {
    const onConfirm =
      mock<(person: unknown, reason: string, role?: string) => void>();
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "replace", outgoing: assignee("Dan", "") }}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));

    expect(onConfirm).toHaveBeenCalledWith(
      { volunteerId: "ada" },
      "",
      HOT_FOOD,
    );
  });

  // The alterations API carries a Role only for a real volunteer, so a choice
  // here would be dropped silently.
  test("someone off the roster is added without a role at all", () => {
    const onConfirm =
      mock<(person: unknown, reason: string, role?: string) => void>();
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "add" }}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Redbridge youth group" },
    });

    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(onConfirm).toHaveBeenCalledWith(
      { custom: "Redbridge youth group" },
      "",
      undefined,
    );
  });

  // Nothing can be sent until the Roles are known, because the API refuses a
  // volunteer arriving without one. Saying so beats a disabled button.
  test("a volunteer cannot be added before the roles have loaded", () => {
    render(
      <AssigneeDialog
        {...assigneeProps()}
        roles={null}
        change={{ kind: "add" }}
        onConfirm={() => {}}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });

    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
  });

  test("the reason field says it is optional", () => {
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "add" }}
        onConfirm={() => {}}
      />,
    );

    expect(screen.getByLabelText("Reason (optional)")).toBeInTheDocument();
  });
});

// One Duty lead, one Hot food and two Greeter places.
const SHAPE = [
  { roleId: "r-duty", role: DUTY_LEAD, count: 1 },
  { roleId: "r-hot", role: HOT_FOOD, count: 1 },
  { roleId: "r-greet", role: GREETER, count: 2 },
];

function pinProps(takenRoleIds: string[] = []) {
  return {
    dateLabel: "Sun 23 Aug",
    volunteers: [grace, ada],
    volunteersError: null,
    seats: seatCounts(SHAPE, takenRoleIds),
    pinnedNames: [],
    busy: false,
    onCancel: () => {},
  };
}

describe("PinDialog", () => {
  // A pin is an instruction to a solve that has not run, so unlike an
  // alteration it is held to every allocator rule (ADR 0009): the roster says
  // which Roles this person may be promised, the Shape says how many are left.
  test("offers only the roles the volunteer holds that the shift has a seat for", () => {
    render(<PinDialog {...pinProps()} onConfirm={() => {}} />);

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });

    expect(optionsOf(screen.getByLabelText("Role"))).toEqual([
      HOT_FOOD,
      GREETER,
    ]);
  });

  test("pins into the chosen seat", () => {
    const onConfirm = mock<(person: unknown, role: string) => void>();
    render(<PinDialog {...pinProps()} onConfirm={onConfirm} />);

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });
    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: GREETER },
    });
    fireEvent.click(screen.getByRole("button", { name: "Pin" }));

    expect(onConfirm).toHaveBeenCalledWith({ volunteerId: "ada" }, GREETER);
  });

  test("a role whose seats are all pinned is not offered, and is named as full", () => {
    render(<PinDialog {...pinProps(["r-hot"])} onConfirm={() => {}} />);

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });

    expect(optionsOf(screen.getByLabelText("Role"))).toEqual([GREETER]);
    expect(screen.getByText(new RegExp(HOT_FOOD))).toBeInTheDocument();
  });

  test("nobody can be pinned once every seat they could fill is taken", () => {
    render(
      <PinDialog {...pinProps(["r-greet", "r-greet"])} onConfirm={() => {}} />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "grace" },
    });

    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();
    expect(screen.getByText(new RegExp(GREETER))).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pin" })).toBeDisabled();
  });

  // #91: an outside provider may be sending anybody, and the API asks nothing
  // about which Roles they hold — only that the Shape has a seat left.
  test("someone off the roster may be pinned into any free seat", () => {
    render(<PinDialog {...pinProps(["r-hot"])} onConfirm={() => {}} />);

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Redbridge youth group" },
    });

    expect(optionsOf(screen.getByLabelText("Role"))).toEqual([
      DUTY_LEAD,
      GREETER,
    ]);
  });

  test("a shift asking for nobody has nothing to pin into", () => {
    render(<PinDialog {...pinProps()} seats={[]} onConfirm={() => {}} />);

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "ada" },
    });

    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pin" })).toBeDisabled();
  });

  // A note, not a refusal: pinning a name twice is how an organisation sending
  // two people is said (issue #195).
  test("pinning a custom name that is already pinned says so, and is allowed", () => {
    render(
      <PinDialog
        {...pinProps()}
        pinnedNames={["Redbridge youth group"]}
        onConfirm={() => {}}
      />,
    );

    fireEvent.change(screen.getByLabelText("Who"), {
      target: { value: "custom" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Redbridge youth group" },
    });

    expect(screen.getByText(/already pinned/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pin" })).toBeEnabled();
  });
});
