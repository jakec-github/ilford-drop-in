import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import { useVolunteers } from "../hooks/useVolunteers";
import SettingsSection from "./SettingsSection";
import type {
  AllocationRule,
  AllocationRuleDraft,
  RuleKind,
  Scope,
  Volunteer,
} from "../types";
import "./AllocationRules.css";

// The Allocation Rules section: the rules an Organiser has added from the Rule
// Library, each with its own value and Scope (ADR 0012). Every word about a
// kind — its label, what it does, what its value means — comes from the library
// the server sends, so a kind arriving or leaving needs no change here.

const EMPTY_SCOPE: Scope = { include: [], exclude: [] };

// A volunteer named on a rule, as the roster knows them — or as an id the
// roster no longer has, which stays on the rule until somebody removes it.
function nameOf(id: string, volunteers: Volunteer[] | null): string {
  const v = volunteers?.find((v) => v.id === id);
  if (!v) return `Volunteer no longer on the roster (${id})`;
  return v.active ? v.fullName : `${v.fullName} (not active)`;
}

// coversNobody is the one Scope that surprises: an include list whose every
// name has left the roster covers nobody — not everyone, as an empty one would.
// Unknowable until the roster has loaded, so said only once it has.
function coversNobody(scope: Scope, volunteers: Volunteer[] | null): boolean {
  if (volunteers === null || scope.include.length === 0) return false;
  return scope.include.every((id) => !volunteers.some((v) => v.id === id));
}

function ScopeChips({
  ids,
  volunteers,
}: {
  ids: string[];
  volunteers: Volunteer[] | null;
}) {
  return (
    <>
      {ids.map((id) => {
        const known = volunteers?.some((v) => v.id === id) ?? true;
        return (
          <span
            key={id}
            className={known ? "scope-chip" : "scope-chip scope-chip--stale"}
          >
            {nameOf(id, volunteers)}
          </span>
        );
      })}
    </>
  );
}

function ScopeSummary({
  scope,
  volunteers,
}: {
  scope: Scope;
  volunteers: Volunteer[] | null;
}) {
  if (scope.include.length > 0) {
    return (
      <span className="rule-scope">
        <span className="rule-scope-lead">Only</span>
        <ScopeChips ids={scope.include} volunteers={volunteers} />
      </span>
    );
  }
  if (scope.exclude.length > 0) {
    return (
      <span className="rule-scope">
        <span className="rule-scope-lead">Everyone except</span>
        <ScopeChips ids={scope.exclude} volunteers={volunteers} />
      </span>
    );
  }
  return (
    <span className="rule-scope">
      <span className="rule-scope-lead">Everyone</span>
    </span>
  );
}

// AllocationRuleList draws the rules as they are stored, in the order they were
// added. A rule the server is not applying is drawn too, said to be ignored, so
// an Organiser can see why it stopped binding and remove it.
export function AllocationRuleList({
  rules,
  library,
  volunteers,
  onEdit,
  onRemove,
}: {
  rules: AllocationRule[];
  library: RuleKind[];
  // null while the roster is still loading.
  volunteers: Volunteer[] | null;
  onEdit: (rule: AllocationRule) => void;
  onRemove: (id: string) => Promise<void>;
}) {
  const [removing, setRemoving] = useState<ReadonlySet<string>>(new Set());
  const [removeError, setRemoveError] = useState<string | null>(null);

  function remove(id: string) {
    setRemoveError(null);
    setRemoving((ids) => new Set(ids).add(id));
    void onRemove(id)
      .catch((err: unknown) => {
        setRemoveError(
          err instanceof Error ? err.message : "Failed to remove the rule",
        );
      })
      .finally(() =>
        setRemoving((ids) => {
          const rest = new Set(ids);
          rest.delete(id);
          return rest;
        }),
      );
  }

  return (
    <>
      {removeError && <p className="settings-error">{removeError}</p>}
      <ul className="allocation-rules">
        {rules.map((rule) => {
          const kind = library.find((k) => k.id === rule.kind) ?? null;
          return (
            <li key={rule.id} className="allocation-rule">
              <div className="allocation-rule-body">
                <span className="allocation-rule-name">
                  {kind ? kind.label : `Unknown rule “${rule.kind}”`}
                </span>
                {kind?.valueLabel && (
                  <span className="allocation-rule-value">
                    {kind.valueLabel}: {rule.value}
                  </span>
                )}
                {kind?.family === "volunteer" && (
                  <ScopeSummary scope={rule.scope} volunteers={volunteers} />
                )}
                {rule.note && (
                  <span className="allocation-rule-note">{rule.note}</span>
                )}
                {rule.ignored && (
                  <span className="rule-flag">
                    Not applied: this version of the app cannot keep it as
                    stored. Remove it, or add the rule again.
                  </span>
                )}
                {!rule.ignored && coversNobody(rule.scope, volunteers) && (
                  <span className="rule-flag">Covers nobody</span>
                )}
              </div>
              <div className="allocation-rule-actions">
                {kind && (
                  <Button size="small" onClick={() => onEdit(rule)}>
                    Edit
                  </Button>
                )}
                <Button
                  size="small"
                  disabled={removing.has(rule.id)}
                  onClick={() => remove(rule.id)}
                >
                  {removing.has(rule.id) ? "Removing…" : "Remove"}
                </Button>
              </div>
            </li>
          );
        })}
      </ul>
    </>
  );
}

// ScopePicker is who a Volunteer Rule covers: two lists, at most one in use. A
// name can be added to one only while the other is empty, so a rule starts out
// covering everyone and is narrowed over time — by leaving people out, or by
// naming the only people it is for.
export function ScopePicker({
  scope,
  volunteers,
  onChange,
}: {
  scope: Scope;
  volunteers: Volunteer[] | null;
  onChange: (scope: Scope) => void;
}) {
  // The include-list name waiting on "this widens the rule" to be confirmed.
  const [widening, setWidening] = useState<string | null>(null);

  function add(list: keyof Scope, id: string) {
    if (!id || scope[list].includes(id)) return;
    onChange({ ...scope, [list]: [...scope[list], id] });
  }

  function remove(list: keyof Scope, id: string) {
    // The last name off an include list leaves both lists empty, which covers
    // everyone: the opposite of what taking somebody off looks like it does.
    if (list === "include" && scope.include.length === 1 && widening !== id) {
      setWidening(id);
      return;
    }
    setWidening(null);
    onChange({ ...scope, [list]: scope[list].filter((x) => x !== id) });
  }

  const named = [...scope.include, ...scope.exclude];
  const inAGroup = named.some(
    (id) => volunteers?.find((v) => v.id === id)?.group,
  );

  function list(which: keyof Scope, label: string, other: keyof Scope) {
    const inUse = scope[other].length === 0;
    return (
      <fieldset className="scope-list">
        <legend>{label}</legend>
        {scope[which].length > 0 && (
          <ul className="scope-names">
            {scope[which].map((id) => (
              <li key={id} className="scope-name">
                <span>{nameOf(id, volunteers)}</span>
                <Button
                  size="small"
                  aria-label={`Remove ${nameOf(id, volunteers)}`}
                  onClick={() => remove(which, id)}
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
        )}
        <select
            aria-label={label}
            value=""
            disabled={!inUse || volunteers === null}
            onChange={(e) => add(which, e.target.value)}
          >
            <option value="">
              {volunteers === null ? "Loading the roster…" : "Add someone…"}
            </option>
            {volunteers
              ?.filter((v) => !scope[which].includes(v.id))
              .map((v) => (
                <option key={v.id} value={v.id}>
                  {nameOf(v.id, volunteers)}
                </option>
              ))}
          </select>
        {!inUse && (
          <p className="settings-hint">Empty the other list to use this one.</p>
        )}
      </fieldset>
    );
  }

  return (
    <div className="scope-picker">
      <p className="settings-hint">
        Name nobody and the rule covers everyone. Then either leave people out,
        or name the only people it is for.
      </p>
      <div className="scope-lists">
        {list("exclude", "Everyone except", "include")}
        {list("include", "Only", "exclude")}
      </div>
      {widening && (
        <div className="settings-warning" role="alert">
          <p>
            {nameOf(widening, volunteers)} is the only volunteer named. Removing
            them makes this rule cover everyone.
          </p>
          <div className="settings-actions">
            <Button size="small" onClick={() => setWidening(null)}>
              Keep
            </Button>
            <Button size="small" onClick={() => remove("include", widening)}>
              Remove and cover everyone
            </Button>
          </div>
        </div>
      )}
      {inAGroup && (
        <p className="settings-hint">
          Couples and families are allocated together, so a rule covering one of
          a group binds the rest of it too.
        </p>
      )}
    </div>
  );
}

// RuleForm is adding one rule of a kind, or editing one: its value, its Scope
// and a note on why. One form for both, because an edit says everything an
// addition does but the kind, which a rule keeps for life.
function RuleForm({
  kind,
  rule,
  volunteers,
  onSave,
  onBack,
  onClose,
}: {
  kind: RuleKind;
  // The rule being edited, or null when one is being added.
  rule: AllocationRule | null;
  volunteers: Volunteer[] | null;
  onSave: (draft: AllocationRuleDraft) => Promise<void>;
  // Back to the library, when adding.
  onBack?: () => void;
  onClose: () => void;
}) {
  // A string so the box can be emptied while it is being retyped.
  const [value, setValue] = useState(
    rule ? String(rule.value) : String(kind.minValue ?? ""),
  );
  const [scope, setScope] = useState<Scope>(rule?.scope ?? EMPTY_SCOPE);
  const [note, setNote] = useState(rule?.note ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const number = Number(value);
  // The bounds the server holds the value to, so an out-of-range one holds Save
  // back rather than earning a refusal (issue #149).
  const valueOk =
    !kind.valueLabel ||
    (value.trim() !== "" &&
      Number.isInteger(number) &&
      number >= (kind.minValue ?? -Infinity) &&
      number <= (kind.maxValue ?? Infinity));
  const sameList = (a: string[], b: string[]) =>
    a.length === b.length && a.every((x, i) => x === b[i]);
  const changed =
    rule === null ||
    number !== rule.value ||
    note.trim() !== rule.note ||
    !sameList(scope.include, rule.scope.include) ||
    !sameList(scope.exclude, rule.scope.exclude);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onSave({
        kind: kind.id,
        value: kind.valueLabel ? number : 0,
        scope: kind.family === "volunteer" ? scope : EMPTY_SCOPE,
        note: note.trim(),
      });
      onClose();
    } catch (err: unknown) {
      // The server's own message names what was wrong, so it is shown as-is
      // and the form stays open on what was entered.
      setError(err instanceof Error ? err.message : "Failed to save the rule");
      setSaving(false);
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <p className="settings-hint rule-kind-description">{kind.description}</p>

      {kind.valueLabel && (
        <label className="settings-field">
          {kind.valueLabel}
          <input
            type="number"
            min={kind.minValue}
            max={kind.maxValue}
            step={1}
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </label>
      )}

      {kind.family === "volunteer" && (
        <ScopePicker scope={scope} volunteers={volunteers} onChange={setScope} />
      )}

      <label className="settings-field">
        Note
        <input
          type="text"
          value={note}
          placeholder="Why this rule is here"
          onChange={(e) => setNote(e.target.value)}
        />
      </label>

      {error && <p className="settings-error">{error}</p>}

      <div className="settings-actions">
        {onBack ? (
          <Button onClick={onBack} disabled={saving}>
            Back
          </Button>
        ) : (
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
        )}
        <Button type="submit" disabled={!valueOk || !changed || saving}>
          {saving ? "Saving…" : rule ? "Save rule" : "Add rule"}
        </Button>
      </div>
    </form>
  );
}

// AddRuleDialog is the library first, then the form for the kind picked from it.
function AddRuleDialog({
  library,
  volunteers,
  onSave,
  onClose,
}: {
  library: RuleKind[];
  volunteers: Volunteer[] | null;
  onSave: (draft: AllocationRuleDraft) => Promise<void>;
  onClose: () => void;
}) {
  const [kind, setKind] = useState<RuleKind | null>(null);

  if (kind) {
    return (
      <Dialog title={kind.label} onClose={onClose}>
        <RuleForm
          kind={kind}
          rule={null}
          volunteers={volunteers}
          onSave={onSave}
          onBack={() => setKind(null)}
          onClose={onClose}
        />
      </Dialog>
    );
  }

  return (
    <Dialog title="Add a rule" onClose={onClose}>
      <p className="settings-hint">
        Any kind can be added more than once — say, a different gap for
        different people.
      </p>
      <ul className="rule-library">
        {library.map((k) => (
          <li key={k.id}>
            <button
              type="button"
              className="rule-library-kind"
              onClick={() => setKind(k)}
            >
              <span className="rule-library-label">{k.label}</span>
              <span className="rule-library-description">{k.description}</span>
            </button>
          </li>
        ))}
      </ul>
      <div className="settings-actions">
        <Button onClick={onClose}>Cancel</Button>
      </div>
    </Dialog>
  );
}

// AllocationRulesSettings is the rules the allocator keeps on top of the ones
// that make a rota a rota, which are always kept and not listed — they are not
// an Organiser's decision to make.
export default function AllocationRulesSettings() {
  const { defaults, error, addRule, editRule, removeRule } = useRotaDefaults();
  const { volunteers } = useVolunteers();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<AllocationRule | null>(null);

  const editingKind =
    editing && defaults
      ? (defaults.ruleLibrary.find((k) => k.id === editing.kind) ?? null)
      : null;

  return (
    <SettingsSection
      title="Allocation rules"
      blurb="The rules the allocator keeps when it fills a rota. They bind the allocator only: a pin is never refused for breaking one."
      action={
        defaults && (
          <Button size="small" onClick={() => setAdding(true)}>
            Add rule
          </Button>
        )
      }
    >
      {error && (
        <p className="settings-error">Could not load the rules: {error}</p>
      )}
      {defaults === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}

      {defaults !== null && defaults.allocationRules.length === 0 && (
        <p className="settings-empty">
          No rules yet, so the allocator keeps only the ones a rota cannot be
          made without.
        </p>
      )}

      {defaults !== null && defaults.allocationRules.length > 0 && (
        <AllocationRuleList
          rules={defaults.allocationRules}
          library={defaults.ruleLibrary}
          volunteers={volunteers}
          onEdit={setEditing}
          onRemove={removeRule}
        />
      )}

      {adding && defaults && (
        <AddRuleDialog
          library={defaults.ruleLibrary}
          volunteers={volunteers}
          onSave={addRule}
          onClose={() => setAdding(false)}
        />
      )}

      {editing && editingKind && (
        <Dialog title={editingKind.label} onClose={() => setEditing(null)}>
          <RuleForm
            kind={editingKind}
            rule={editing}
            volunteers={volunteers}
            onSave={({ value, scope, note }) =>
              editRule(editing.id, { value, scope, note })
            }
            onClose={() => setEditing(null)}
          />
        </Dialog>
      )}
    </SettingsSection>
  );
}
