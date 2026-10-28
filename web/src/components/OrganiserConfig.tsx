import { useEffect, useRef, useState, type PointerEvent } from "react";
import DragHandle from "../ui/DragHandle";
import IconButton from "../ui/IconButton";
import { useDragReorder } from "../ui/useDragReorder";
import {
  useAutosave,
  useDraft,
  useSaveWhenSettled,
} from "../hooks/useAutosave";
import { useRoles } from "../hooks/useRoles";
import { useSetup } from "../hooks/useSetup";
import { useVolunteers, type SyncState } from "../hooks/useVolunteers";
import AllocationRulesSettings from "./AllocationRules";
import OverridesCard from "./OverridesCard";
import RotaDefaultsCard from "./RotaDefaultsCard";
import SaveStatus from "./SaveStatus";
import SettingsSection from "./SettingsSection";
import type { ConfiguredRole, RoleColour, RoleEdit, Volunteer } from "../types";
import { DEFAULT_ROLE_COLOUR, ROLE_COLOURS } from "../types";
import "./OrganiserConfig.css";

// The colour a new Role starts on: the dullest token, so an Organiser who does not
// care about colour does not accidentally claim a distinctive one.
const NEW_ROLE_COLOUR: RoleColour = DEFAULT_ROLE_COLOUR;

// What a Role card edits. Priority is not here: it is the card's place in the
// list, and moving the card is how it changes.
export interface RoleLook {
  name: string;
  colour: RoleColour;
}

const sameLook = (a: RoleLook, b: RoleLook) =>
  a.name.trim() === b.name.trim() && a.colour === b.colour;

// How many active volunteers hold a Role. The roster names Roles by string, so
// this is the count a rename moves to nought until the sheet is changed too.
function holdersOf(name: string, volunteers: Volunteer[]): number {
  return volunteers.filter((v) => v.active && v.roles.includes(name)).length;
}

// ColourPicker is a Role's colour as a row of swatches. The radio is the
// control; the token's name is kept for a screen reader and drawn only as the
// swatch's title.
function ColourPicker({
  name,
  colour,
  disabled,
  onChange,
}: {
  name: string;
  colour: RoleColour;
  disabled?: boolean;
  onChange: (colour: RoleColour) => void;
}) {
  return (
    <fieldset className="settings-swatches settings-swatches--compact">
      <legend className="settings-swatches-legend">Colour of {name}</legend>
      {ROLE_COLOURS.map((token) => (
        <label
          key={token}
          className="settings-swatch"
          data-role-colour={token}
          title={token}
        >
          <input
            type="radio"
            name={`colour-${name}`}
            value={token}
            checked={colour === token}
            disabled={disabled}
            onChange={() => onChange(token)}
          />
          <span className="settings-swatch-dot" aria-hidden="true" />
          <span className="settings-swatch-name">{token}</span>
        </label>
      ))}
    </fieldset>
  );
}

// RoleCard is one Role, edited where it stands: its name and colour save
// themselves, and its place in the list is its priority. There is no remove:
// a Role is permanent (ADR 0006).
export function RoleCard({
  role,
  place,
  count,
  holders,
  busy,
  held,
  onSave,
  onGrab,
  onMove,
}: {
  role: ConfiguredRole;
  // 0-based, as a drag would leave it.
  place: number;
  count: number;
  // null while the roster is loading.
  holders: number | null;
  busy: boolean;
  held: boolean;
  onSave: (look: RoleLook, renamed: boolean) => Promise<void>;
  onGrab: (e: PointerEvent<HTMLButtonElement>) => void;
  onMove: (by: -1 | 1) => void;
}) {
  const { draft, setDraft, saved } = useDraft({
    source: role,
    toDraft: (r: ConfiguredRole): RoleLook => ({
      name: r.name,
      colour: r.colour,
    }),
    same: sameLook,
  });
  const name = draft.name.trim();
  const { saveState, error } = useSaveWhenSettled({
    draft,
    saved,
    same: sameLook,
    save: (look) => onSave(look, look.name.trim() !== saved.current.name),
    ready: name !== "",
  });
  // The name the card opened on: a rename is only half done until the sheet
  // says the same, so it is flagged for as long as the card is up.
  const [openedAs] = useState(role.name);
  const renamed = name !== "" && name !== openedAs;

  return (
    <article
      className={`role-card${held ? " role-card--dragging" : ""}`}
      data-role-colour={draft.colour}
      aria-label={role.name}
    >
      <div className="role-card-main">
        <span className="role-card-place" aria-hidden="true">
          {place + 1}
        </span>
        <input
          className="role-card-name"
          type="text"
          aria-label={`Name of ${role.name}`}
          value={draft.name}
          onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
        />
        <span className="role-card-holders">
          {holders === null
            ? "…"
            : `${holders} ${holders === 1 ? "volunteer" : "volunteers"}`}
        </span>
        <SaveStatus state={saveState} />
        <DragHandle
          label={`Reorder ${role.name}`}
          title="Drag to reorder: places are filled from the top. Arrow keys move it a place."
          place={place}
          count={count}
          busy={busy}
          onGrab={onGrab}
          onMove={onMove}
        />
      </div>
      <ColourPicker
        name={role.name}
        colour={draft.colour}
        onChange={(colour) => setDraft((d) => ({ ...d, colour }))}
      />
      {renamed && (
        <p className="settings-warning" role="status">
          Renaming only changes this app. The roster sheet holds Roles by name,
          so change “{openedAs}” to “{name}” there too — until you do, nobody
          holds this Role.
        </p>
      )}
      {name === "" && <p className="settings-error">A Role needs a name.</p>}
      {error && <p className="settings-error">{error}</p>}
    </article>
  );
}

// NewRoleCard is a Role being written for the first time. It exists only
// here until it has a name, and is added the moment the name stops moving.
export function NewRoleCard({
  onAdd,
  onDiscard,
}: {
  onAdd: (look: RoleLook) => Promise<void>;
  onDiscard: () => void;
}) {
  const [blank] = useState<RoleLook>({ name: "", colour: NEW_ROLE_COLOUR });
  const { draft, setDraft, saveState, error } = useAutosave({
    source: blank,
    toDraft: (look: RoleLook) => look,
    // Only a name makes it worth adding: a colour picked first waits for one.
    same: (a, b) => a.name.trim() === b.name.trim(),
    save: (look) => onAdd({ ...look, name: look.name.trim() }),
  });
  const box = useRef<HTMLInputElement>(null);
  useEffect(() => box.current?.focus(), []);

  return (
    <article
      className="role-card role-card--new"
      data-role-colour={draft.colour}
      aria-label="New role"
    >
      <div className="role-card-main">
        <input
          ref={box}
          className="role-card-name"
          type="text"
          aria-label="Name of the new role"
          placeholder="e.g. Food collector"
          value={draft.name}
          onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
        />
        <SaveStatus state={saveState} />
        <button
          type="button"
          className="role-card-discard"
          aria-label="Discard the new role"
          title="Discard"
          onClick={onDiscard}
        >
          <svg viewBox="0 0 20 20" aria-hidden="true">
            <circle cx="10" cy="10" r="8" />
            <path d="M6 10h8" />
          </svg>
        </button>
      </div>
      <ColourPicker
        name="the new role"
        colour={draft.colour}
        onChange={(colour) => setDraft((d) => ({ ...d, colour }))}
      />
      {error && <p className="settings-error">{error}</p>}
    </article>
  );
}

// What re-reading the roster after a Role change is doing, said under the list.
const SYNC_NOTE: Record<SyncState, string> = {
  idle: "",
  syncing: "Re-reading the roster so volunteers pick up the change…",
  ok: "Roster re-read.",
  error: "Could not re-read the roster. Sync it from the Volunteers tab.",
};

// RolesSettings is the Roles section: the jobs volunteers hold, in the order
// their places are filled, each edited where it stands.
//
// The roster is re-read after any change to which Roles exist, what they are
// called or their order: the sheet names Roles by string and lists them in
// priority order, so each of those changes who holds what.
function RolesSettings({ incomplete }: { incomplete: boolean }) {
  const { roles, error, addRole, saveRole } = useRoles();
  const { volunteers, syncState, sync } = useVolunteers();
  const [adding, setAdding] = useState(false);
  // The order being written, held over the list while the writes land one by
  // one, so it does not shuffle through every order in between.
  const [writing, setWriting] = useState<string[] | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const byId = new Map((roles ?? []).map((r) => [r.id, r]));
  const shown =
    writing && roles
      ? writing.flatMap((id) => {
          const r = byId.get(id);
          return r ? [r] : [];
        })
      : roles;
  const ids = shown?.map((r) => r.id) ?? null;

  // Priority is the place in the list, rewritten from the top for every Role
  // whose place moved. Priorities need not be unique, so a write that fails
  // half way leaves an order that still works, and the next move puts it right.
  async function reorder(order: string[]) {
    setWriting(order);
    setActionError(null);
    try {
      for (const [i, id] of order.entries()) {
        const role = byId.get(id);
        if (role && role.priority !== i + 1) {
          await saveRole(id, {
            name: role.name,
            colour: role.colour,
            priority: i + 1,
          });
        }
      }
      void sync();
    } catch (err: unknown) {
      setActionError(
        err instanceof Error ? err.message : "Failed to reorder the roles",
      );
    } finally {
      setWriting(null);
    }
  }

  const drag = useDragReorder(ids, (order) => void reorder(order));

  // A new Role goes after the ones that exist, so adding one never silently
  // reorders the filling of everything else.
  const nextPriority =
    roles && roles.length > 0
      ? Math.max(...roles.map((r) => r.priority)) + 1
      : 1;

  async function save(role: ConfiguredRole, look: RoleLook, renamed: boolean) {
    const edit: RoleEdit = {
      name: look.name.trim(),
      colour: look.colour,
      priority: role.priority,
    };
    await saveRole(role.id, edit);
    if (renamed) void sync();
  }

  async function add(look: RoleLook) {
    await addRole({ ...look, priority: nextPriority });
    setAdding(false);
    void sync();
  }

  return (
    <SettingsSection
      title="Roles"
      blurb="The jobs volunteers do on a shift, in the order their places are filled. A volunteer can only be given a Role they hold on the roster."
      incomplete={incomplete}
      action={
        roles !== null &&
        !adding && (
          <IconButton
            icon="plus"
            variant="filled"
            aria-label="New role"
            onClick={() => setAdding(true)}
          />
        )
      }
    >
      {error && <p className="settings-error">Could not load roles: {error}</p>}
      {actionError && <p className="settings-error">{actionError}</p>}

      {roles === null && !error && <p className="settings-empty">Loading…</p>}

      {roles !== null && roles.length === 0 && !adding && (
        <p className="settings-empty">
          No roles yet. Add the jobs volunteers do at the organisation — a rota
          cannot be made without them.
        </p>
      )}

      {shown !== null && shown.length > 0 && (
        <ol {...drag.listProps}>
          {shown.map((role) => (
            <li key={role.id} {...drag.itemProps(role.id)}>
              <RoleCard
                role={role}
                place={drag.placeOf(role.id)}
                count={shown.length}
                holders={volunteers ? holdersOf(role.name, volunteers) : null}
                busy={writing !== null}
                held={drag.held === role.id}
                onSave={(look, renamed) => save(role, look, renamed)}
                onGrab={(e) => drag.grab(role.id, e)}
                onMove={(by) => drag.moveBy(role.id, by)}
              />
            </li>
          ))}
        </ol>
      )}

      {adding && <NewRoleCard onAdd={add} onDiscard={() => setAdding(false)} />}

      <p className={`roles-sync roles-sync--${syncState}`} aria-live="polite">
        {SYNC_NOTE[syncState]}
      </p>
    </SettingsSection>
  );
}

// OrganiserConfig is everything an Organiser decides about how the Organisation
// runs, as opposed to what an operator sets when deploying it (ADR 0006).
//
// Roles and the Rota card come first and are the whole of setup: a rota cannot
// be made without them, so the sections that refine one — the Overrides and
// the Allocation Rules — stay closed until both are set.
//
// The Rota and Overrides cards are not only here — the define screen shows the
// same components, because defining a rota is spending them (issue #176).
export default function OrganiserConfig() {
  const setup = useSetup();
  return (
    <>
      <RolesSettings incomplete={setup.roles === false} />
      <RotaDefaultsCard />
      {setup.done === true && (
        <>
          <OverridesCard />
          <AllocationRulesSettings />
        </>
      )}
      {setup.done === false && (
        <p className="config-locked">
          Overrides and allocation rules open once the Roles and the Rota are
          set.
        </p>
      )}
    </>
  );
}
