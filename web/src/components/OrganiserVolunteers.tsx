import { wholeShares } from "./wholeShares";
import { useMemo } from "react";
import Button from "../ui/Button";
import type { RoleColourOf, RoleIdOf } from "../hooks/useRoles";
import { useRoles } from "../hooks/useRoles";
import { useVolunteers, type SyncState } from "../hooks/useVolunteers";
import type { Volunteer } from "../types";
import "./OrganiserVolunteers.css";

// The sync caption doubles as its own outcome message, so the line under the
// button is always exactly one line and the layout never jumps.
const SYNC_CAPTION: Record<SyncState, string> = {
  idle: "Re-reads the Google Sheet",
  syncing: "Re-reading the Google Sheet…",
  ok: "Roster up to date",
  error: "Sync failed — please try again",
};

interface RosterCounts {
  activeVolunteers: number;
  // One entry per Role anybody active holds, in priority order. Derived from
  // the roster rather than from a list of configured Roles, because nothing
  // tells the frontend what those are yet — a Role nobody holds does not appear.
  byRole: { role: string; count: number }[];
  // Each share of the active volunteers, in percent, or null when nobody is
  // active — there is then no denominator, and a dash says so where 0% would
  // read as a fact. other is everyone the sheet calls neither, blank included,
  // so the three always make the whole.
  genderSplit: { male: number; female: number; other: number } | null;
}

// Gender is free text from the sheet, so "male" and "female" are matched
// case-insensitively and anything else — "Prefer not to say", blank — is
// other. One definition, used by both the split and the tag, so the stat can
// never disagree with the rows it is counting.
function isMale(volunteer: Volunteer): boolean {
  return volunteer.gender?.toLowerCase() === "male";
}

function isFemale(volunteer: Volunteer): boolean {
  return volunteer.gender?.toLowerCase() === "female";
}

// Every count is over active volunteers only: an Organiser sizing up the team wants
// who can actually be rostered, not who has ever been on the sheet. The per-Role
// counts are subsets of that same total, not separate populations, and they
// overlap each other — somebody who will lead and will do an ordinary shift is
// counted under both, so these do not add up to the total.
function countRoster(volunteers: Volunteer[]): RosterCounts {
  const active = volunteers.filter((v) => v.active);
  const male = active.filter(isMale).length;
  const female = active.filter(isFemale).length;

  const counts = new Map<string, number>();
  // A volunteer's Roles arrive in priority order, so a lower-priority Role is
  // always further down some list than a higher-priority one. Ranking each Role
  // by the furthest down it ever appears recovers the configured order from the
  // roster alone, which is the only place the frontend can read it until S3.
  const rank = new Map<string, number>();
  for (const volunteer of active) {
    volunteer.roles.forEach((role, index) => {
      counts.set(role, (counts.get(role) ?? 0) + 1);
      rank.set(role, Math.max(rank.get(role) ?? 0, index));
    });
  }

  return {
    activeVolunteers: active.length,
    byRole: [...counts]
      .map(([role, count]) => ({ role, count }))
      .sort((a, b) => (rank.get(a.role) ?? 0) - (rank.get(b.role) ?? 0)),
    genderSplit:
      active.length === 0
        ? null
        : (([m, f, o]) => ({ male: m, female: f, other: o }))(
            wholeShares(
              [male, female, active.length - male - female],
              active.length,
            ),
          ),
  };
}

// GenderSplit is the roster's gender mix as one bar, its parts labelled
// beneath. Other is drawn only when somebody is in it, so a roster that is all
// male and female reads as two parts rather than three with one empty.
function GenderSplit({ split }: { split: RosterCounts["genderSplit"] }) {
  const parts = split
    ? [
        { key: "male", label: "Male", share: split.male },
        { key: "female", label: "Female", share: split.female },
        ...(split.other > 0
          ? [{ key: "other", label: "Other", share: split.other }]
          : []),
      ]
    : [];
  return (
    <div className="roster-count roster-count--split">
      <dt>Gender split</dt>
      <dd>
        {split === null ? (
          "—"
        ) : (
          <>
            <span className="gender-bar" aria-hidden="true">
              {parts.map((p) => (
                <span
                  key={p.key}
                  className={`gender-bar-part gender-bar-part--${p.key}`}
                  style={{ flexGrow: p.share }}
                />
              ))}
            </span>
            <span className="gender-legend">
              {parts.map((p) => (
                <span
                  key={p.key}
                  className={`gender-legend-item gender-legend-item--${p.key}`}
                >
                  {p.label} {p.share}%
                </span>
              ))}
            </span>
            <span className="roster-count-note">of active volunteers</span>
          </>
        )}
      </dd>
    </div>
  );
}

function Count({
  label,
  value,
  note,
}: {
  label: string;
  value: string;
  note?: string;
}) {
  return (
    <div className="roster-count">
      <dt>{label}</dt>
      <dd>
        {value}
        {/* Inside the dd, not beside it: a dl's div may only hold dt/dd, and a
            screen reader then reads the figure and its denominator together. */}
        {note && <span className="roster-count-note">{note}</span>}
      </dd>
    </div>
  );
}

// One roster entry: the full name, then only the tags that mark someone out from
// the default. A female volunteer and an ungrouped volunteer are each the common
// case, so tagging them would put a label on nearly every row and leave nothing
// standing out. What remains is what an Organiser scans for.
//
// Not being active is one of those exceptions, so it is tagged as well as dimmed —
// the tag is what carries the state to a screen reader, which cannot see dimming.
function RosterRow({
  volunteer,
  colourOf,
  idOf,
}: {
  volunteer: Volunteer;
  colourOf: RoleColourOf;
  // The roster spells a Role out by name, and a colour is the Role's own, so
  // the name is read as the Role it names before it is coloured.
  idOf: RoleIdOf;
}) {
  return (
    <li
      className={`roster-row${volunteer.active ? "" : " roster-row--inactive"}`}
    >
      <span className="roster-name">{volunteer.fullName}</span>
      <span className="roster-tags">
        {/* Every Role held, not just the lead one: a roster row should say
            what somebody will actually do. Each wears its configured colour,
            so a Role looks the same here as it does on the rota. */}
        {volunteer.roles.map((role) => (
          <span
            key={role}
            className="roster-tag roster-tag--role"
            data-role-colour={colourOf(idOf(role) ?? "") ?? undefined}
          >
            {role}
          </span>
        ))}
        {isMale(volunteer) && <span className="roster-tag">Male</span>}
        {volunteer.group && (
          <span className="roster-tag" title="Group">
            {volunteer.group}
          </span>
        )}
        {!volunteer.active && (
          <span className="roster-tag roster-tag--inactive">Not active</span>
        )}
      </span>
    </li>
  );
}

// OrganiserVolunteers is the volunteers tab: the synced roster, a summary of it, and
// the button that re-syncs it. The sync sits top right and stays small — it is
// an occasional maintenance action, not the point of the page.
export default function OrganiserVolunteers() {
  const { volunteers, error, syncState, sync } = useVolunteers();
  // A Role wears its configured colour here as well as on the rota, so a lead
  // looks like a lead wherever they appear.
  const { colourOf, idOf } = useRoles();
  const counts = useMemo(
    () => (volunteers ? countRoster(volunteers) : null),
    [volunteers],
  );

  return (
    <section className="organiser-panel volunteers">
      <header className="volunteers-head">
        <h2>Volunteers</h2>
        <div className="volunteers-sync">
          <Button
            size="small"
            onClick={() => void sync()}
            disabled={syncState === "syncing"}
          >
            {syncState === "syncing" ? "Syncing…" : "Sync"}
          </Button>
          <p
            className={`volunteers-sync-caption volunteers-sync-caption--${syncState}`}
            aria-live="polite"
          >
            {SYNC_CAPTION[syncState]}
          </p>
        </div>
      </header>

      {error && (
        <p className="volunteers-message volunteers-message--error">
          Could not load the roster: {error}
        </p>
      )}

      {counts && volunteers && volunteers.length > 0 && (
        <>
          <dl className="roster-counts">
            <Count
              label="Active volunteers"
              value={String(counts.activeVolunteers)}
            />
            {counts.byRole.map(({ role, count }) => (
              <Count key={role} label={role} value={String(count)} />
            ))}
            <GenderSplit split={counts.genderSplit} />
          </dl>

          <p className="roster-caption">
            All {volunteers.length} volunteers on the sheet.
          </p>
          <ul className="roster">
            {volunteers.map((v) => (
              <RosterRow
                key={v.id}
                volunteer={v}
                colourOf={colourOf}
                idOf={idOf}
              />
            ))}
          </ul>
        </>
      )}

      {!error && volunteers === null && (
        <p className="volunteers-message">Loading roster…</p>
      )}
      {volunteers !== null && volunteers.length === 0 && (
        <p className="volunteers-message">
          No volunteers yet. Sync to pull the roster in.
        </p>
      )}
    </section>
  );
}
