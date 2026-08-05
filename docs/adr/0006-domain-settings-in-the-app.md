# Domain settings live in the app, not in the config file

Status: accepted

Everything an admin decides about how the drop-in runs — the Roles that exist,
the default Shape, shift times, which optional allocator rules apply, the
standing preallocations — moves out of `drop_in_config.yaml` and into the
database, edited on an admin screen. The config file keeps only what an
*operator* sets when deploying: sheet ids, database URL, gmail, server, dev
mode, admin emails. The dividing line is who changes it and why, not how often.

The settings are **live and global**. A Rotation does not carry a copy of the
settings it was allocated under, and there is no history of which toggles
produced which rota. That record was considered and dropped deliberately: an
allocated rota is materialised as `allocation` rows, so live settings cannot
retroactively rewrite one, and the only thing lost is provenance. Carrying
provenance would tax every future change to the constraint set, which is the
part of the system most expected to change.

## Decisions and their reasons

- **Allocation settings are `jsonb`; the constraint registry in code stays the
  authority for which toggles exist.** Constraints will come and go, and a
  schema column per constraint would make each arrival a migration. A stored
  answer for a constraint that no longer exists is ignored with a warning,
  never an error — the rule the config loader learned on 3 August 2026, when
  strict key decoding took the site down. A constraint with no stored answer is
  **off**, so nothing switches itself on behind an admin's back.

- **The default Shape is rows, not JSON.** It is the one setting that
  references Roles, and a live Shape must never dangle. Rows carry a foreign
  key; JSON cannot. It also matches `shift_requirement`, which holds the same
  thing per Shift.

- **Roles get an identity separate from their name, and are permanent.** Once
  created a Role always exists — there is no deletion and no retirement. That
  keeps the model as small as it can be: no lifecycle state, no "is this Role
  still offered?" question at any of the dozen places a Role is read, and no
  reference that can dangle. The cost is a picker that accumulates Roles the
  drop-in has stopped using, which is a cosmetic problem and a cheap one to
  solve later if it ever bites. `allocation.role`, `alteration.role` and
  `preallocation.role` still keep the name as `TEXT`, so a past rota reads as it
  was made even after a rename.

- **Renaming a Role is allowed but flagged.** The roster is a Google Sheet and
  a volunteer's held Roles are names in a cell, so the app owns only half of
  the name contract. A rename is surfaced at the point of rename, and the
  existing roster validation warnings become the standing check.

- **No migration seeds the settings.** Admins set them. Invalid or incomplete
  settings block **allocation** specifically; nothing else breaks. The one
  exception is shift times, which are written onto existing Shifts so nothing
  that renders a time meets a null.

## Considered and rejected

- **Leaving `roles` in YAML** while shapes and settings moved. Rejected on the
  grounds that it leaves a stored Shape referencing a name in a file nobody
  validates against, and splits one idea across two authorities.

- **Recording per-Rotation settings, frozen at allocation.** Symmetric with
  Shapes, and it would answer "how was this rota made?". Rejected as a
  permanent tax for an answer nobody has needed, on the values most likely to
  churn.

- **Porting `rotaOverrides` as recurrence rules.** Rrules existed because there
  was no UI. With Shapes editable per Shift, `Closed` a field on the Shift, and
  pins added on screen, the rules have nothing left to do. Standing
  preallocations survive as Rota Defaults that **seed ordinary pins when a
  Rotation is defined** — a convenience at definition, not a fact that outranks
  the admin.

## Consequences

- **Config Preallocations cease to exist**, and with them the authority rule
  that a manual pin could never remove one (ADR 0003). There is one kind of
  Preallocation. The pin-source branching carried through the API and UI by #87,
  #92 and #109 is deleted — less code afterwards than before, but it does unpick
  recent work.

- `validate_config` shrinks to checking deployment keys.

- The admin area gains a settings screen, which the stubbed `/admin/config` tab
  has been holding a place for.
