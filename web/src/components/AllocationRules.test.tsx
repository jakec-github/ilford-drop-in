import { describe, expect, mock, test } from "bun:test";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { AllocationRuleList, ScopePicker } from "./AllocationRules";
import type { AllocationRule, RuleKind, Scope, Volunteer } from "../types";

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
};
const COVER: RuleKind = {
  id: "male_required",
  family: "shift",
  label: "Cover on every shift",
  description: "Every shift has cover.",
};
const LIBRARY = [GAP, COVER];

function volunteer(id: string, fullName: string, extra: Partial<Volunteer> = {}): Volunteer {
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
const ada = volunteer("ada", "Ada Lovelace", { active: false });
const ROSTER = [grace, ada];

function rule(id: string, scope: Partial<Scope>, extra: Partial<AllocationRule> = {}): AllocationRule {
  return {
    id,
    kind: GAP.id,
    value: 2,
    scope: { include: [], exclude: [], ...scope },
    note: "",
    ignored: false,
    ...extra,
  };
}

function renderList(rules: AllocationRule[]) {
  const onEdit = mock(() => {});
  const onRemove = mock(async () => {});
  render(
    <AllocationRuleList
      rules={rules}
      library={LIBRARY}
      volunteers={ROSTER}
      onEdit={onEdit}
      onRemove={onRemove}
    />,
  );
  return { onEdit, onRemove };
}

describe("AllocationRuleList", () => {
  test("names who a rule covers, and that an empty Scope is everyone", () => {
    renderList([
      rule("everyone", {}),
      rule("only", { include: ["grace"] }),
      rule("except", { exclude: ["ada"] }),
    ]);

    const rows = screen.getAllByRole("listitem");
    expect(rows[0].textContent).toContain("Everyone");
    expect(within(rows[1]).getByText(/^Only/)).toBeTruthy();
    expect(within(rows[1]).getByText("Grace Hopper")).toBeTruthy();
    expect(within(rows[2]).getByText(/^Everyone except/)).toBeTruthy();
    expect(within(rows[2]).getByText("Ada Lovelace (not active)")).toBeTruthy();
  });

  // A volunteer who has left the roster stays on the rule, said as such, with
  // the id so an Organiser can tell two leavers apart.
  test("flags a volunteer no longer on the roster", () => {
    renderList([rule("stale", { exclude: ["gone-1"] })]);

    const stale = screen.getByText(/Volunteer no longer on the roster/);
    expect(stale.textContent).toContain("gone-1");
    expect(screen.queryByText("Covers nobody")).toBeNull();
  });

  // An include list of leavers covers nobody — not everyone — and that is the
  // surprise worth saying out loud.
  test("flags an include list whose every volunteer has left as covering nobody", () => {
    renderList([
      rule("nobody", { include: ["gone-1", "gone-2"] }),
      rule("somebody", { include: ["gone-1", "grace"] }),
    ]);

    const rows = screen.getAllByRole("listitem");
    expect(within(rows[0]).getByText("Covers nobody")).toBeTruthy();
    expect(within(rows[1]).queryByText("Covers nobody")).toBeNull();
  });

  // A rule this build cannot apply is shown, said to be ignored, and can be
  // removed — but not edited, since there is no form for a kind it lacks.
  test("shows an ignored rule as not applied, removable only", () => {
    const { onRemove } = renderList([
      rule("unknown", {}, { kind: "phase_of_the_moon", ignored: true }),
    ]);

    const unknown = screen.getAllByRole("listitem")[0];
    expect(within(unknown).getByText(/Not applied/)).toBeTruthy();
    expect(within(unknown).queryByRole("button", { name: /Edit/ })).toBeNull();
    fireEvent.click(within(unknown).getByRole("button", { name: /Remove/ }));
    expect(onRemove).toHaveBeenCalledWith("unknown");
  });

  test("shows a Shift Rule without a Scope", () => {
    renderList([rule("cover", {}, { kind: COVER.id, value: 0 })]);

    const cover = screen.getAllByRole("listitem")[0];
    expect(within(cover).getByText(COVER.label)).toBeTruthy();
    expect(within(cover).queryByText(/Everyone/)).toBeNull();
  });
});

describe("ScopePicker", () => {
  function renderPicker(scope: Scope) {
    const onChange = mock<(scope: Scope) => void>(() => {});
    render(<ScopePicker scope={scope} volunteers={ROSTER} onChange={onChange} />);
    return onChange;
  }

  // A name can go on one list only while the other is empty.
  test("offers only the list in use once somebody is named", () => {
    renderPicker({ include: ["grace"], exclude: [] });

    const include = screen.getByRole("combobox", { name: /Only/ });
    const exclude = screen.getByRole("combobox", { name: /Everyone except/ });
    expect((include as HTMLSelectElement).disabled).toBe(false);
    expect((exclude as HTMLSelectElement).disabled).toBe(true);
  });

  test("offers the whole roster, inactive volunteers included", () => {
    const onChange = renderPicker({ include: [], exclude: [] });

    const exclude = screen.getByRole("combobox", { name: /Everyone except/ });
    expect(within(exclude).getByRole("option", { name: "Ada Lovelace (not active)" })).toBeTruthy();
    fireEvent.change(exclude, { target: { value: "ada" } });
    expect(onChange).toHaveBeenCalledWith({ include: [], exclude: ["ada"] });
  });

  // Emptying an include list widens the rule to everyone, which is the
  // opposite of what removing somebody looks like it does — so it asks first.
  test("asks before removing the last included volunteer widens the rule", () => {
    const onChange = renderPicker({ include: ["grace"], exclude: [] });

    fireEvent.click(screen.getByRole("button", { name: "Remove Grace Hopper" }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(/cover everyone/);

    fireEvent.click(screen.getByRole("button", { name: /Remove and cover everyone/ }));
    expect(onChange).toHaveBeenCalledWith({ include: [], exclude: [] });
  });

  test("removes from an exclude list without asking", () => {
    const onChange = renderPicker({ include: [], exclude: ["grace"] });

    fireEvent.click(screen.getByRole("button", { name: "Remove Grace Hopper" }));
    expect(onChange).toHaveBeenCalledWith({ include: [], exclude: [] });
  });
});
