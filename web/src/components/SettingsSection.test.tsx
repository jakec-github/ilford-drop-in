import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsSection from "./SettingsSection";
import { settingsSummary } from "./settingsSummary";

function section(props: {
  incomplete?: boolean;
  onAction?: () => void;
  collapsible?: boolean;
}) {
  return (
    <SettingsSection
      title="Things"
      blurb="What the things are."
      collapsible={props.collapsible ?? true}
      incomplete={props.incomplete}
      summary="2 things"
      action={
        <button type="button" onClick={props.onAction}>
          New thing
        </button>
      }
    >
      <p>The list</p>
    </SettingsSection>
  );
}

describe("SettingsSection", () => {
  test("a collapsible section starts closed, showing its summary", () => {
    render(section({}));
    const toggle = screen.getByRole("button", { name: /Things/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByText("2 things")).toBeInTheDocument();
    expect(screen.getByText("The list")).not.toBeVisible();

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("The list")).toBeVisible();
    expect(screen.queryByText("2 things")).not.toBeInTheDocument();
  });

  test("the action stays while closed, and opens the section as it acts", () => {
    const onAction = mock(() => {});
    render(section({ onAction }));
    fireEvent.click(screen.getByRole("button", { name: "New thing" }));
    expect(onAction).toHaveBeenCalled();
    expect(screen.getByText("The list")).toBeVisible();
  });

  test("a section setup still needs cannot be closed", () => {
    render(section({ incomplete: true }));
    expect(screen.queryByRole("button", { name: /Things/ })).toBeNull();
    expect(screen.getByText("The list")).toBeVisible();
  });

  test("once filled in, it stays open rather than shutting", () => {
    const { rerender } = render(section({ incomplete: true }));
    rerender(section({ incomplete: false }));
    expect(screen.getByRole("button", { name: /Things/ })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByText("The list")).toBeVisible();
  });

  test("a section that is not collapsible is always open", () => {
    render(section({ collapsible: false }));
    expect(screen.queryByRole("button", { name: /Things/ })).toBeNull();
    expect(screen.getByText("The list")).toBeVisible();
  });
});

describe("settingsSummary", () => {
  test("counts, says None, and says nothing while loading", () => {
    expect(settingsSummary(undefined, "role", "roles")).toBeUndefined();
    expect(settingsSummary(0, "role", "roles")).toBe("None");
    expect(settingsSummary(1, "role", "roles")).toBe("1 role");
    expect(settingsSummary(3, "role", "roles")).toBe("3 roles");
  });
});
