import { useMemo } from "react";
import Button from "../ui/Button";
import { useVolunteers, type SyncState } from "../hooks/useVolunteers";
import type { Volunteer } from "../types";
import "./AdminVolunteers.css";

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
  activeTeamLeads: number;
  // null when nobody is active, since there is then no denominator to take a
  // percentage of — shown as a dash rather than 0%, which would read as a fact.
  malePercentage: number | null;
}

// Every count is over active volunteers only: an admin sizing up the team wants
// who can actually be rostered, not who has ever been on the sheet. Team leads
// are a subset of that same total, not a separate population — a lead is a
// volunteer holding the lead role.
//
// Gender is free text from the sheet, so "male" is matched case-insensitively
// and anything else — "Female", "Prefer not to say", blank — simply is not male.
function countRoster(volunteers: Volunteer[]): RosterCounts {
  const active = volunteers.filter((v) => v.active);
  const male = active.filter((v) => v.gender?.toLowerCase() === "male").length;

  return {
    activeVolunteers: active.length,
    activeTeamLeads: active.filter((v) => v.role === "lead").length,
    malePercentage:
      active.length === 0 ? null : Math.round((male / active.length) * 100),
  };
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

// One roster entry: the name, then the facts about them as tags. Role is tagged
// on every row rather than only on leads — an admin reading a roster should not
// have to infer a role from the absence of a label.
function RosterRow({ volunteer }: { volunteer: Volunteer }) {
  return (
    <li
      className={`roster-row${volunteer.active ? "" : " roster-row--inactive"}`}
    >
      <span className="roster-name">{volunteer.name}</span>
      <span className="roster-tags">
        <span
          className={`roster-tag roster-tag--role-${volunteer.role}`}
          title="Role"
        >
          {volunteer.role === "lead" ? "Team lead" : "Service volunteer"}
        </span>
        {volunteer.gender && (
          <span className="roster-tag" title="Gender">
            {volunteer.gender}
          </span>
        )}
        {volunteer.group && (
          <span className="roster-tag" title="Group">
            {volunteer.group}
          </span>
        )}
        {!volunteer.active && (
          <span className="roster-tag roster-tag--inactive">Inactive</span>
        )}
      </span>
    </li>
  );
}

// AdminVolunteers is the volunteers tab: the synced roster, a summary of it, and
// the button that re-syncs it. The sync sits top right and stays small — it is
// an occasional maintenance action, not the point of the page.
export default function AdminVolunteers() {
  const { volunteers, error, syncState, sync } = useVolunteers();
  const counts = useMemo(
    () => (volunteers ? countRoster(volunteers) : null),
    [volunteers],
  );

  return (
    <section className="admin-panel volunteers">
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
            <Count
              label="Active team leads"
              value={String(counts.activeTeamLeads)}
            />
            <Count
              label="Male"
              value={
                counts.malePercentage === null
                  ? "—"
                  : `${counts.malePercentage}%`
              }
              note="of active volunteers"
            />
          </dl>

          <p className="roster-caption">
            All {volunteers.length} volunteers on the sheet, including those who
            have left.
          </p>
          <ul className="roster">
            {volunteers.map((v) => (
              <RosterRow key={v.id} volunteer={v} />
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
