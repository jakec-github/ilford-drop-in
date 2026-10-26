import { describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { AllocationRuleCards } from "./AllocationRules";
import type {
  AllocationRule,
  AllocationRuleEdit,
  RuleKind,
  Scope,
  Volunteer,
} from "../types";

// Kinds as the server's library describes them. The screen draws whatever it
// is sent, so these are worded unlike the real library on purpose.
const GAP: RuleKind = {
  id: "shift_gap",
  family: "volunteer",
  label: "Rest between shifts",
  description: "Shifts off between two worked.",
  valueLabel: "Shifts off",
  minValue: 1,
  maxValue: 52,
  input: "stepper",
};
const SHARE: RuleKind = {
  id: "max_frequency",
  family: "volunteer",
  label: "Share of shifts",
  description: "At most this share.",
  valueLabel: "Share",
  minValue: 1,
  maxValue: 100,
  input: "percent",
};
const MONTHLY: RuleKind = {
  id: "monthly_cap",
  family: "volunteer",
  label: "Shifts a month",
  description: "At most this many a month.",
  valueLabel: "Most a month",
  minValue: 1,
  maxValue: 31,
  input: "count",
};
const COVER: RuleKind = {
  id: "shift_requires",
  family: "shift",
  label: "Cover on every shift",
  description: "Every shift has cover.",
  optionLabel: "Kind of cover",
  options: ["Day", "Night"],
};
const LIBRARY = [GAP, SHARE, MONTHLY, COVER];

// Long enough for a card to stop waiting for more movement and save.
const SAVED = 700;
const wait = (ms: number) =>
  act(() => new Promise<void>((resolve) => setTimeout(resolve, ms)));

function volunteer(
  id: string,
  fullName: string,
  extra: Partial<Volunteer> = {},
): Volunteer {
  return {
    id,
    name: fullName.split(" ")[0],
    fullName,
    roles: [],
    group: null,
    gender: null,
    active: true,
    ...extra,
  };
}

const grace = volunteer("grace", "Grace Hopper");
const alan = volunteer("alan", "Alan Turing");
const ada = volunteer("ada", "Ada Lovelace", { active: false });
const ROSTER = [grace, alan, ada];

function rule(
  id: string,
  scope: Partial<Scope> = {},
  extra: Partial<AllocationRule> = {},
): AllocationRule {
  return {
    id,
    kind: GAP.id,
    value: 2,
    option: "",
    scope: { include: [], exclude: [], ...scope },
    note: "",
    ignored: false,
    ...extra,
  };
}

function renderCards(rules: AllocationRule[]) {
  const onSave = mock<(id: string, edit: AllocationRuleEdit) => Promise<void>>(
    async () => {},
  );
  const onRemove = mock<(id: string) => Promise<void>>(async () => {});
  render(
    <AllocationRuleCards
      rules={rules}
      library={LIBRARY}
      volunteers={ROSTER}
      onSave={onSave}
      onRemove={onRemove}
    />,
  );
  return { onSave, onRemove };
}

const card = (name: string) => screen.getByRole("listitem", { name });

describe("a rule card", () => {
  test("names who it covers as chips, and how many that is", () => {
    renderCards([
      rule("everyone"),
      rule("except", { exclude: ["grace"] }, { kind: SHARE.id, value: 50 }),
    ]);

    expect(
      within(card("Rest between shifts")).getByText(
        "Covers every active volunteer",
      ),
    ).toBeTruthy();

    const except = card("Share of shifts");
    expect(within(except).getByText("Grace Hopper")).toBeTruthy();
    expect(
      within(except)
        .getByRole("radio", { name: "Everyone except" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      within(except).getByText("Covers 1 of 2 active volunteers"),
    ).toBeTruthy();
  });

  test("saves a change once the control stops moving, as one write", async () => {
    const { onSave } = renderCards([rule("gap")]);
    const more = screen.getByRole("button", { name: "More: Shifts off" });

    fireEvent.click(more);
    fireEvent.click(more);
    expect(onSave).not.toHaveBeenCalled();

    await wait(SAVED);
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0]).toEqual([
      "gap",
      { value: 4, option: "", scope: { include: [], exclude: [] }, note: "" },
    ]);
  });

  test("keeps a typed value within what the kind accepts", async () => {
    const { onSave } = renderCards([rule("gap")]);
    const figure = screen.getByRole("textbox", { name: "Shifts off" });

    fireEvent.focus(figure);
    fireEvent.change(figure, { target: { value: "999" } });
    fireEvent.blur(figure);

    await wait(SAVED);
    expect(onSave.mock.calls[0][1].value).toBe(52);
  });

  test("moves the percentage ring from the keyboard", async () => {
    const { onSave } = renderCards([
      rule("share", {}, { kind: SHARE.id, value: 50 }),
    ]);
    const ring = screen.getByRole("slider", { name: "Share" });

    fireEvent.keyDown(ring, { key: "ArrowUp" });
    fireEvent.keyDown(ring, { key: "PageUp" });
    expect(ring.getAttribute("aria-valuenow")).toBe("61");

    await wait(SAVED);
    expect(onSave.mock.calls[0][1].value).toBe(61);
  });

  test("grows the slider's scale when its end is reached", () => {
    renderCards([rule("monthly", {}, { kind: MONTHLY.id, value: 2 })]);
    const slider = screen.getByRole("slider", { name: "Most a month, slider" });
    expect(slider.getAttribute("max")).toBe("8");

    fireEvent.change(slider, { target: { value: "8" } });
    expect(slider.getAttribute("max")).toBe("16");

    // And shrinks back once the value is well inside the smaller scale.
    fireEvent.change(slider, { target: { value: "3" } });
    expect(slider.getAttribute("max")).toBe("8");
  });

  test("sets a kind's choice from the options it offers", async () => {
    const { onSave } = renderCards([
      rule("cover", {}, { kind: COVER.id, value: 0, option: "Day" }),
    ]);
    const cover = card("Cover on every shift");
    // A Shift Rule has no Scope to show.
    expect(within(cover).queryByText(/Covers/)).toBeNull();

    fireEvent.click(within(cover).getByRole("radio", { name: "Night" }));
    await wait(SAVED);
    expect(onSave.mock.calls[0][1].option).toBe("Night");
  });

  test("flipping who it covers takes the names across", async () => {
    const { onSave } = renderCards([rule("gap", { exclude: ["grace"] })]);

    fireEvent.click(screen.getByRole("radio", { name: "Only" }));
    await wait(SAVED);
    expect(onSave.mock.calls[0][1].scope).toEqual({
      include: ["grace"],
      exclude: [],
    });
  });

  test("offers inactive volunteers too, and adds the one picked", async () => {
    const { onSave } = renderCards([rule("gap")]);
    const add = screen.getByRole("combobox", { name: "Everyone except" });
    expect(
      within(add).getByRole("option", { name: "Ada Lovelace (not active)" }),
    ).toBeTruthy();

    fireEvent.change(add, { target: { value: "alan" } });
    await wait(SAVED);
    expect(onSave.mock.calls[0][1].scope).toEqual({
      include: [],
      exclude: ["alan"],
    });
  });

  test("warns before the last name off an include list widens it to everyone", async () => {
    const { onSave } = renderCards([rule("gap", { include: ["grace"] })]);

    fireEvent.click(
      screen.getByRole("button", { name: "Remove Grace Hopper" }),
    );
    expect(screen.getByRole("alert").textContent).toContain("cover everyone");
    await wait(SAVED);
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: /Remove and cover everyone/ }),
    );
    await wait(SAVED);
    expect(onSave.mock.calls[0][1].scope).toEqual({ include: [], exclude: [] });
  });

  test("asks on the card before removing it", async () => {
    const { onRemove } = renderCards([rule("gap")]);
    const gap = card("Rest between shifts");

    fireEvent.click(
      within(gap).getByRole("button", { name: "Remove Rest between shifts" }),
    );
    fireEvent.click(within(gap).getByRole("button", { name: "Keep" }));
    expect(onRemove).not.toHaveBeenCalled();

    fireEvent.click(
      within(gap).getByRole("button", { name: "Remove Rest between shifts" }),
    );
    fireEvent.click(within(gap).getByRole("button", { name: "Remove" }));
    expect(onRemove).toHaveBeenCalledWith("gap");
  });

  // A kind this build does not have is drawn so it can be seen and removed —
  // but not edited, since nothing here knows what its value means.
  test("draws a rule of an unknown kind with nothing to edit", () => {
    renderCards([
      rule("unknown", {}, { kind: "phase_of_the_moon", ignored: true }),
    ]);
    const unknown = card("Unknown rule “phase_of_the_moon”");

    expect(within(unknown).getByText(/Not applied/)).toBeTruthy();
    expect(within(unknown).queryByRole("textbox")).toBeNull();
    expect(
      within(unknown).getByRole("button", { name: /Remove/ }),
    ).toBeTruthy();
  });

  test("says when it covers nobody because everyone named has left", () => {
    renderCards([rule("gap", { include: ["gone"] })]);
    expect(screen.getByText(/Covers nobody/)).toBeTruthy();
  });
});
