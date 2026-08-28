import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import { ConfirmChangeDialog } from "./RotaEditDialogs";
import { SERVICE_VOLUNTEER_ROLE, TEAM_LEAD_ROLE } from "../types";

// ConfirmChangeDialog is the confirmation an admin sees after dragging (or
// tapping) a volunteer onto a shift — issue #146. Its role field is the
// feature: no field for a remove or a swap (no `role` prop passed), a real
// choice for a move onto a shift with a free lead seat, and a fixed answer,
// stated rather than offered, once that seat is already taken.
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

  test("a move onto a shift with a free lead seat offers a role choice, defaulted to what the volunteer was already doing", () => {
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: TEAM_LEAD_ROLE, leadTaken: false }}
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
        role={{ initial: TEAM_LEAD_ROLE, leadTaken: false }}
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

  test("a move onto a shift whose lead seat is already taken states the outcome instead of offering a choice", () => {
    const onConfirm = mock<(reason: string, role?: string) => void>();
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: TEAM_LEAD_ROLE, leadTaken: true }}
        onConfirm={onConfirm}
      />,
    );

    expect(screen.queryByLabelText("Role")).not.toBeInTheDocument();
    expect(screen.getByText(/already has a team lead/)).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("e.g. away that week"), {
      target: { value: "moved anyway" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Move" }));

    // Forced to service volunteer even though `initial` said Team lead — the
    // seat is gone, so the admin's choice was never really theirs to make.
    expect(onConfirm).toHaveBeenCalledWith(
      "moved anyway",
      SERVICE_VOLUNTEER_ROLE,
    );
  });

  test("the confirm button stays disabled until a reason is entered", () => {
    render(
      <ConfirmChangeDialog
        {...baseProps()}
        role={{ initial: SERVICE_VOLUNTEER_ROLE, leadTaken: false }}
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
