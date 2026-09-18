import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import { AssigneeDialog, ConfirmChangeDialog } from "./RotaEditDialogs";
import type { Volunteer } from "../types";
import { SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";

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
        role={{ initial: TEAM_LEAD_ROLE }}
        onConfirm={() => {}}
      />,
    );

    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    expect(roleField.value).toBe(TEAM_LEAD_ROLE);
  });

  test("changing the role choice and confirming passes the chosen role, not the initial one", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: TEAM_LEAD_ROLE }}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: SERVICE_VOLUNTEER_ROLE },
    });
    fireEvent.change(screen.getByPlaceholderText("e.g. away that week"), {
      target: { value: "covering, not leading" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Move" }));

    expect(onConfirm).toHaveBeenCalledWith(
      "covering, not leading",
      SERVICE_VOLUNTEER_ROLE,
    );
  });

  test("any role can be picked, regardless of who else already holds it", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: SERVICE_VOLUNTEER_ROLE }}
        onConfirm={onConfirm}
      />,
    );

    const roleField = screen.getByLabelText("Role") as HTMLSelectElement;
    fireEvent.change(roleField, { target: { value: TEAM_LEAD_ROLE } });
    fireEvent.change(screen.getByPlaceholderText("e.g. away that week"), {
      target: { value: "moved anyway" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Move" }));

    expect(onConfirm).toHaveBeenCalledWith("moved anyway", TEAM_LEAD_ROLE);
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

const grace: Volunteer = {
  id: "grace",
  name: "Grace",
  fullName: "Grace Hopper",
  roles: [SERVICE_VOLUNTEER_ROLE],
  group: null,
  gender: null,
  active: true,
};

function assigneeProps() {
  return {
    dateLabel: "Sun 23 Aug",
    volunteers: [grace],
    volunteersError: null,
    busy: false,
    onCancel: () => {},
  };
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
        change={{ kind: "add", leadTaken: false }}
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
      SERVICE_VOLUNTEER_ROLE,
    );
  });

  test("the reason field says it is optional", () => {
    render(
      <AssigneeDialog
        {...assigneeProps()}
        change={{ kind: "add", leadTaken: false }}
        onConfirm={() => {}}
      />,
    );

    expect(screen.getByLabelText("Reason (optional)")).toBeInTheDocument();
  });
});
