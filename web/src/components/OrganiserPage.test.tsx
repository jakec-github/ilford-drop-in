import { describe, expect, mock, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import { AuthContext, type AccessLevel } from "../auth-context";
import type { OrganiserTab } from "./organiserTabs";

// The real tab list imports every panel, and through them ../api — which
// another test file replaces with a partial mock for the whole run. Nothing
// here is about the panels, only whether the shell lets anyone reach one.
mock.module("./organiserTabs", () => ({
  ORGANISER_TABS: [{ path: "/organiser/volunteers", label: "Volunteers" }],
}));

const { default: OrganiserPage } = await import("./OrganiserPage");

const TAB: OrganiserTab = {
  path: "/organiser/volunteers",
  label: "Volunteers",
  Panel: () => <p>The panel</p>,
};

function renderAs(level: AccessLevel | null) {
  render(
    <AuthContext.Provider
      value={{
        email: level === null ? null : "someone@example.com",
        level,
        loading: false,
        logout: async () => {},
      }}
    >
      <OrganiserPage tab={TAB} />
    </AuthContext.Provider>,
  );
}

describe("OrganiserPage", () => {
  test("an Organiser reaches the panel", () => {
    renderAs("organiser");

    expect(
      screen.getByRole("heading", { name: "Organiser" }),
    ).toBeInTheDocument();
    expect(screen.getByText("The panel")).toBeInTheDocument();
  });

  test("a Rota Editor sees nothing of the area, and is not asked to log in", () => {
    renderAs("rotaEditor");

    expect(screen.queryByText("The panel")).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Log in" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/for organisers/)).toBeInTheDocument();
  });

  test("logged out, it offers a login", () => {
    renderAs(null);

    expect(screen.queryByText("The panel")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Log in" })).toBeInTheDocument();
  });
});
