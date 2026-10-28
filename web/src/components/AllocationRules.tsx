import { useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import Button from "../ui/Button";
import Chip from "../ui/Chip";
import IconButton from "../ui/IconButton";
import { personKey } from "./shifts";
import Dialog from "../ui/Dialog";
import { useAutosave } from "../hooks/useAutosave";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import { useVolunteers } from "../hooks/useVolunteers";
import RuleValueInput from "./RuleValueInput";
import SaveStatus from "./SaveStatus";
import SettingsSection from "./SettingsSection";
import { settingsSummary } from "./settingsSummary";
import type {
  AllocationRule,
  AllocationRuleDraft,
  AllocationRuleEdit,
  RuleKind,
  Scope,
  Volunteer,
} from "../types";
import "./AllocationRules.css";

// The Allocation Rules: the rules an Organiser has added from the Rule Library,
// each a card edited where it stands (ADR 0012, issue #281). Every word about a
// kind — its label, what it does, what its value means, which control sets it
// — comes from the library the server sends, so a kind arriving or leaving
// needs no change here.
//
// The same cards are drawn on the settings screen and beside the draft rota.
// They are the same rules in both places: there is one set, and editing it
// beside the draft is editing it everywhere.

const EMPTY_SCOPE: Scope = { include: [], exclude: [] };

// The colours kinds are told apart by, given out in library order. Which
// colour is which kind is not a fact about the kind, only a way to tell two
// cards apart at a glance.
const KIND_COLOURS = [
  "var(--role-violet)",
  "var(--role-teal)",
  "var(--role-amber)",
  "var(--role-blue)",
  "var(--role-rose)",
  "var(--role-green)",
];

function colourOf(kind: RuleKind | null, library: RuleKind[]): string {
  if (!kind) return "var(--role-slate)";
  return KIND_COLOURS[library.indexOf(kind) % KIND_COLOURS.length];
}

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

// How many of the active roster a Scope covers, said as a sentence.
function coverage(scope: Scope, volunteers: Volunteer[] | null): string | null {
  if (volunteers === null) return null;
  const active = volunteers.filter((v) => v.active);
  const covered = active.filter((v) =>
    scope.include.length > 0
      ? scope.include.includes(v.id)
      : !scope.exclude.includes(v.id),
  ).length;
  if (covered === active.length) return "Covers every active volunteer";
  return `Covers ${covered} of ${active.length} active volunteers`;
}

// What a card holds while it is being changed: everything an edit sends.
function editOf(rule: AllocationRule): AllocationRuleEdit {
  return {
    value: rule.value,
    option: rule.option,
    scope: rule.scope,
    note: rule.note,
  };
}

function sameEdit(a: AllocationRuleEdit, b: AllocationRuleEdit): boolean {
  const sameList = (x: string[], y: string[]) =>
    x.length === y.length && x.every((v, i) => v === y[i]);
  return (
    a.value === b.value &&
    a.option === b.option &&
    a.note.trim() === b.note.trim() &&
    sameList(a.scope.include, b.scope.include) &&
    sameList(a.scope.exclude, b.scope.exclude)
  );
}

// ScopeEditor is who a Volunteer Rule covers, edited in place: a switch saying
// whether the names are the only people covered or the people left out, and
// the names themselves as chips. A Scope uses one list at most (ADR 0012), so
// flipping the switch moves the names across rather than keeping two.
export function ScopeEditor({
  scope,
  volunteers,
  disabled,
  onChange,
}: {
  scope: Scope;
  volunteers: Volunteer[] | null;
  disabled: boolean;
  onChange: (scope: Scope) => void;
}) {
  // Somebody's removal waiting on "this widens the rule" to be confirmed.
  const [widening, setWidening] = useState<string | null>(null);
  const list: keyof Scope = scope.include.length > 0 ? "include" : "exclude";
  // With nobody named the switch is still worth having: it says which list the
  // next name goes on. Held here until a name makes it the Scope's own.
  const [mode, setMode] = useState<keyof Scope>(list);
  const active = scope.include.length + scope.exclude.length > 0 ? list : mode;
  const named = scope[active];

  function flip(next: keyof Scope) {
    setMode(next);
    if (next === active || named.length === 0) return;
    // "Only Mark" and "Everyone except Mark" name the same person for opposite
    // reasons: the names go with the switch.
    onChange({ ...EMPTY_SCOPE, [next]: named });
  }

  function add(id: string) {
    if (!id || named.includes(id)) return;
    onChange({ ...EMPTY_SCOPE, [active]: [...named, id] });
  }

  function remove(id: string) {
    // The last name off an include list leaves both lists empty, which covers
    // everyone: the opposite of what taking somebody off looks like it does.
    if (active === "include" && named.length === 1 && widening !== id) {
      setWidening(id);
      return;
    }
    setWidening(null);
    onChange({ ...EMPTY_SCOPE, [active]: named.filter((x) => x !== id) });
  }

  const inAGroup = named.some(
    (id) => volunteers?.find((v) => v.id === id)?.group,
  );

  return (
    <div className="scope-editor">
      <div className="scope-editor-row">
        <div
          className="scope-switch"
          role="radiogroup"
          aria-label="Who it covers"
        >
          {(
            [
              ["exclude", "Everyone except"],
              ["include", "Only"],
            ] as const
          ).map(([which, label]) => (
            <button
              key={which}
              type="button"
              role="radio"
              aria-checked={active === which}
              className={active === which ? "on" : ""}
              disabled={disabled}
              onClick={() => flip(which)}
            >
              {label}
            </button>
          ))}
        </div>
        {named.map((id) => {
          const known = volunteers?.some((v) => v.id === id) ?? true;
          return (
            <Chip
              key={id}
              label={nameOf(id, volunteers)}
              selectionKey={personKey({ volunteerId: id })}
              className={known ? undefined : "chip--stale"}
              onRemove={disabled ? undefined : () => remove(id)}
            />
          );
        })}
        <select
          className="scope-add"
          aria-label={active === "include" ? "Only" : "Everyone except"}
          value=""
          disabled={disabled || volunteers === null}
          onChange={(e) => add(e.target.value)}
        >
          <option value="">
            {volunteers === null ? "Loading…" : "+ person"}
          </option>
          {volunteers
            ?.filter((v) => !named.includes(v.id))
            .map((v) => (
              <option key={v.id} value={v.id}>
                {nameOf(v.id, volunteers)}
              </option>
            ))}
        </select>
      </div>
      {widening && (
        <div className="rule-inline-confirm" role="alert">
          <p>
            {nameOf(widening, volunteers)} is the only volunteer named. Removing
            them makes this rule cover everyone.
          </p>
          <div className="settings-actions">
            <Button size="small" onClick={() => setWidening(null)}>
              Keep
            </Button>
            <Button size="small" onClick={() => remove(widening)}>
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

// RuleCard is one rule, edited where it stands. Changes save themselves once
// the control stops moving; removing asks first, on the card itself.
export function RuleCard({
  rule,
  kind,
  colour,
  volunteers,
  compact,
  fresh,
  onSave,
  onRemove,
}: {
  rule: AllocationRule;
  // null for a kind this build does not have, which can only be removed.
  kind: RuleKind | null;
  colour: string;
  volunteers: Volunteer[] | null;
  // Beside the draft rota: the description goes, and the card is narrower.
  compact: boolean;
  // Just added from the library: the card announces itself.
  fresh: boolean;
  onSave: (id: string, edit: AllocationRuleEdit) => Promise<void>;
  onRemove: (id: string) => Promise<void>;
}) {
  const { draft, setDraft, saveState, error, setError } = useAutosave({
    source: rule,
    toDraft: editOf,
    same: sameEdit,
    save: (edit) => onSave(rule.id, { ...edit, note: edit.note.trim() }),
  });
  const [asking, setAsking] = useState(false);
  const [removing, setRemoving] = useState(false);
  const card = useRef<HTMLLIElement>(null);

  useEffect(() => {
    if (fresh)
      card.current?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
  }, [fresh]);

  const editable = kind !== null && !rule.ignored && !removing;
  const label = kind ? kind.label : `Unknown rule “${rule.kind}”`;
  const change = (part: Partial<AllocationRuleEdit>) =>
    setDraft((d) => ({ ...d, ...part }));

  function remove() {
    setRemoving(true);
    setError(null);
    onRemove(rule.id).catch((err: unknown) => {
      setRemoving(false);
      setAsking(false);
      setError(err instanceof Error ? err.message : "The rule was not removed");
    });
  }

  const classes = [
    "rule-card",
    compact ? "rule-card--compact" : "",
    fresh ? "rule-card--fresh" : "",
    asking ? "rule-card--asking" : "",
    removing ? "rule-card--leaving" : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <li
      ref={card}
      className={classes}
      style={{ "--rule-colour": colour } as CSSProperties}
      aria-label={label}
    >
      <header className="rule-card-head">
        <h3 className="rule-card-name">{label}</h3>
        <SaveStatus state={saveState} />
        <button
          type="button"
          className="rule-card-remove"
          aria-label={`Remove ${label}`}
          disabled={removing}
          onClick={() => setAsking(true)}
        >
          <svg viewBox="0 0 20 20" aria-hidden="true">
            <circle cx="10" cy="10" r="8" />
            <path d="M6 10h8" />
          </svg>
        </button>
      </header>

      {!compact && kind && (
        <p className="rule-card-description">{kind.description}</p>
      )}

      {kind?.valueLabel && (
        <RuleValueInput
          kind={kind}
          value={draft.value}
          disabled={!editable}
          onChange={(value) => change({ value })}
        />
      )}

      {kind?.options && kind.options.length > 0 && (
        <div className="rule-option">
          <span className="rule-option-label" id={`${rule.id}-option`}>
            {kind.optionLabel}
          </span>
          <div
            className="scope-switch"
            role="radiogroup"
            aria-labelledby={`${rule.id}-option`}
          >
            {kind.options.map((option) => (
              <button
                key={option}
                type="button"
                role="radio"
                aria-checked={draft.option === option}
                className={draft.option === option ? "on" : ""}
                disabled={!editable}
                onClick={() => change({ option })}
              >
                {option}
              </button>
            ))}
          </div>
        </div>
      )}

      {kind?.family === "volunteer" && (
        <>
          <ScopeEditor
            scope={draft.scope}
            volunteers={volunteers}
            disabled={!editable}
            onChange={(scope) => change({ scope })}
          />
          <p className="rule-card-coverage">
            {coversNobody(draft.scope, volunteers)
              ? "Covers nobody: every name on it has left the roster"
              : coverage(draft.scope, volunteers)}
          </p>
        </>
      )}

      {kind && (
        <input
          className="rule-card-note"
          type="text"
          aria-label="Note"
          placeholder="Add a note: why this rule is here"
          value={draft.note}
          disabled={!editable}
          onChange={(e) => change({ note: e.target.value })}
        />
      )}

      {rule.ignored && (
        <p className="rule-flag">
          Not applied: this version of the app cannot keep it as stored. Remove
          it, or add the rule again.
        </p>
      )}
      {error && <p className="settings-error">{error}</p>}

      <div className="rule-card-confirm" aria-hidden={!asking}>
        {asking && (
          <>
            <span>Remove this rule?</span>
            <Button size="small" onClick={() => setAsking(false)}>
              Keep
            </Button>
            <Button
              size="small"
              className="rule-card-confirm-remove"
              disabled={removing}
              onClick={remove}
            >
              {removing ? "Removing…" : "Remove"}
            </Button>
          </>
        )}
      </div>
    </li>
  );
}

// RuleLibraryDialog is every kind there is to add. Picking one adds it at once,
// at the value its kind starts at, and the card it becomes is where it is
// changed: there is no form in between.
function RuleLibraryDialog({
  library,
  onPick,
  onClose,
}: {
  library: RuleKind[];
  onPick: (draft: AllocationRuleDraft) => Promise<void>;
  onClose: () => void;
}) {
  const [adding, setAdding] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  function pick(kind: RuleKind) {
    setAdding(kind.id);
    setError(null);
    onPick({
      kind: kind.id,
      value: kind.valueLabel ? (kind.defaultValue ?? kind.minValue ?? 0) : 0,
      option: kind.options?.[0] ?? "",
      scope: EMPTY_SCOPE,
      note: "",
    })
      .then(onClose)
      .catch((err: unknown) => {
        setAdding(null);
        setError(err instanceof Error ? err.message : "The rule was not added");
      });
  }

  return (
    <Dialog title="Rule library" onClose={onClose}>
      <p className="settings-hint">
        Any rule can be added more than once — say, a different gap for
        different people.
      </p>
      <ul className="rule-library">
        {library.map((k) => (
          <li key={k.id}>
            <button
              type="button"
              className="rule-library-kind"
              style={{ "--rule-colour": colourOf(k, library) } as CSSProperties}
              disabled={adding !== null}
              onClick={() => pick(k)}
            >
              <span className="rule-library-label">
                {adding === k.id ? "Adding…" : k.label}
              </span>
              <span className="rule-library-description">{k.description}</span>
            </button>
          </li>
        ))}
      </ul>
      {error && <p className="settings-error">{error}</p>}
      <div className="settings-actions">
        <Button onClick={onClose}>Cancel</Button>
      </div>
    </Dialog>
  );
}

// AllocationRuleCards draws the rules as they are stored, in the order they
// were added. A rule the server is not applying is drawn too, said to be
// ignored, so an Organiser can see why it stopped binding and remove it.
export function AllocationRuleCards({
  rules,
  library,
  volunteers,
  compact = false,
  freshId = null,
  onSave,
  onRemove,
}: {
  rules: AllocationRule[];
  library: RuleKind[];
  // null while the roster is still loading.
  volunteers: Volunteer[] | null;
  compact?: boolean;
  freshId?: string | null;
  onSave: (id: string, edit: AllocationRuleEdit) => Promise<void>;
  onRemove: (id: string) => Promise<void>;
}) {
  return (
    <ul className={compact ? "rule-cards rule-cards--compact" : "rule-cards"}>
      {rules.map((rule) => {
        const kind = library.find((k) => k.id === rule.kind) ?? null;
        return (
          <RuleCard
            key={rule.id}
            rule={rule}
            kind={kind}
            colour={colourOf(kind, library)}
            volunteers={volunteers}
            compact={compact}
            fresh={rule.id === freshId}
            onSave={onSave}
            onRemove={onRemove}
          />
        );
      })}
    </ul>
  );
}

// useAllocationRules is the rules with everything both screens do to them:
// load, add from the library, edit, remove. onChanged runs after every write
// that landed, which is how the draft rota hears that an input moved.
function useAllocationRules(onChanged?: () => void) {
  const { defaults, error, addRule, editRule, removeRule } = useRotaDefaults();
  const { volunteers } = useVolunteers();
  const [freshId, setFreshId] = useState<string | null>(null);
  const known = useRef<Set<string>>(new Set());
  const changed = useRef(onChanged);
  useEffect(() => {
    changed.current = onChanged;
  }, [onChanged]);

  // The id of a rule just added is only known from the list that comes back,
  // as the one that was not there before.
  useEffect(() => {
    const ids = (defaults?.allocationRules ?? []).map((r) => r.id);
    if (known.current.size > 0) {
      const added = ids.find((id) => !known.current.has(id));
      if (added) setFreshId(added);
    }
    known.current = new Set(ids);
  }, [defaults]);

  const told = () => changed.current?.();
  return {
    defaults,
    error,
    volunteers,
    freshId,
    // Seeded once the first list is in, so the rules already there on load
    // are not taken for ones just added.
    add: (draft: AllocationRuleDraft) => {
      if (known.current.size === 0) known.current.add("");
      return addRule(draft).then(told);
    },
    edit: (id: string, edit: AllocationRuleEdit) =>
      editRule(id, edit).then(told),
    remove: (id: string) => removeRule(id).then(told),
  };
}

function AddRuleButton({ onClick }: { onClick: () => void }) {
  return (
    <IconButton
      icon="plus"
      variant="filled"
      aria-label="Add a rule from the rule library"
      title="Rule library"
      onClick={onClick}
    />
  );
}

// AllocationRulesSettings is the rules the allocator keeps on top of the ones
// that make a rota a rota, which are always kept and not listed — they are not
// an Organiser's decision to make.
export default function AllocationRulesSettings() {
  const { defaults, error, volunteers, freshId, add, edit, remove } =
    useAllocationRules();
  const [browsing, setBrowsing] = useState(false);

  return (
    <SettingsSection
      title="Allocation rules"
      collapsible
      summary={settingsSummary(
        defaults?.allocationRules.length,
        "rule",
        "rules",
      )}
      blurb="The rules the allocator keeps when it fills a rota. They bind the allocator only: a pin is never refused for breaking one."
      action={defaults && <AddRuleButton onClick={() => setBrowsing(true)} />}
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
        <AllocationRuleCards
          rules={defaults.allocationRules}
          library={defaults.ruleLibrary}
          volunteers={volunteers}
          freshId={freshId}
          onSave={edit}
          onRemove={remove}
        />
      )}

      {browsing && defaults && (
        <RuleLibraryDialog
          library={defaults.ruleLibrary}
          onPick={add}
          onClose={() => setBrowsing(false)}
        />
      )}
    </SettingsSection>
  );
}

// DraftRules is the same rules beside the draft rota, so the rota and what it
// was made under are read together. Every change here is an allocator input,
// so it tells the draft, which re-solves around it.
export function DraftRules({ onChanged }: { onChanged: () => void }) {
  const { defaults, error, volunteers, freshId, add, edit, remove } =
    useAllocationRules(onChanged);
  const [browsing, setBrowsing] = useState(false);

  return (
    <aside
      className="organiser-panel draft-rules"
      aria-label="Allocation rules"
    >
      <header className="draft-rules-head">
        <h2>Allocation rules</h2>
        {defaults && <AddRuleButton onClick={() => setBrowsing(true)} />}
      </header>
      {error && (
        <p className="settings-error">Could not load the rules: {error}</p>
      )}
      {defaults === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}
      {defaults !== null && defaults.allocationRules.length === 0 && (
        <p className="settings-empty">
          No rules: the draft keeps only the ones a rota cannot be made without.
        </p>
      )}
      {defaults !== null && defaults.allocationRules.length > 0 && (
        <AllocationRuleCards
          rules={defaults.allocationRules}
          library={defaults.ruleLibrary}
          volunteers={volunteers}
          compact
          freshId={freshId}
          onSave={edit}
          onRemove={remove}
        />
      )}
      {browsing && defaults && (
        <RuleLibraryDialog
          library={defaults.ruleLibrary}
          onPick={add}
          onClose={() => setBrowsing(false)}
        />
      )}
    </aside>
  );
}
