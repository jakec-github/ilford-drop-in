import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import { ConfirmChangeDialog } from "./RotaEditDialogs";
import { SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";

function baseProps() {
  return {
    title: "Move Grace?",
    summary: "Grace moves from Sun 23 Aug to Sun 30 Aug.",
    confirmLabel: "Move",
    busy: false,
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

  test("the confirm button stays disabled until a reason is entered", () => {
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: SERVICE_VOLUNTEER_ROLE }}
        onConfirm={() => {}}
      />,
    );

    expect(screen.getByRole("button", { name: "Move" })).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("e.g. away that week"), {
      target: { value: "away that week" },
    });

    expect(screen.getByRole("button", { name: "Move" })).toBeEnabled();
  });
});
