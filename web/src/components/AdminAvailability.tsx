import { useState, type ReactNode } from "react";
import Button from "../ui/Button";
import { useAvailabilityRound } from "../hooks/useAvailabilityRound";
import type {
  AvailabilityEntry,
  AvailabilityGroup,
  AvailabilityRound,
  ShiftCoverage,
} from "../types";
import "./AdminAvailability.css";

function formatRange(round: AvailabilityRound): string {
  const format = (date: string) =>
    new Date(date).toLocaleDateString("en-GB", {
      day: "numeric",
      month: "short",
    });
  return `${format(round.start)} – ${format(round.end)}`;
}

function formatDate(date: string): string {
  return new Date(date).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
  });
}

// The short form for the availability chips, where the same dates repeat once
// per group and the weekday is just noise.
function formatShortDate(date: string): string {
  return new Date(date).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
  });
}

// One date's staffing, which is what this view is for: enough people, and
// somebody allowed to lead.
//
// The delta carries the colour rather than the counts, because it is the thing
// being scanned for — an admin reads down a column of signed numbers looking for
// the negative ones. A shift that is exactly full stays neutral: it is fine, and
// colouring it too would leave nothing standing out.
function CoverageRow({ shift }: { shift: ShiftCoverage }) {
  if (shift.closed) {
    return (
      <li className="cover-row cover-row--closed">
        <span className="cover-date">{formatDate(shift.date)}</span>
        <span className="cover-counts">Closed</span>
      </li>
    );
  }

  return (
    <li className="cover-row">
      <span className="cover-date">{formatDate(shift.date)}</span>
      <span className="cover-counts">
        {shift.available} available of {shift.needed} needed
        {/* Without this an admin sees a date asking for fewer people than the
            config says, with nothing to explain why. */}
        {shift.pinned > 0 && (
          <span className="cover-note"> · {shift.pinned} already pinned</span>
        )}
      </span>
      <span className="cover-tags">
        <span
          className={`cover-tag ${
            shift.delta < 0 ? "cover-tag--short" : "cover-tag--ok"
          }`}
        >
          {shift.delta > 0 ? `+${shift.delta}` : shift.delta}
        </span>
        {!shift.hasTeamLead && (
          <span className="cover-tag cover-tag--short">No team lead</span>
        )}
      </span>
    </li>
  );
}

// The URL and the button that copies it. The URL is shown in full as well as
// copied: copying fails silently on an insecure origin or a locked-down browser,
// and an admin who can read it can always select it by hand.
function CopyableLink({ link }: { link: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <div className="member-link">
      <code className="member-link-url">{link}</code>
      <Button
        size="small"
        onClick={() => {
          void navigator.clipboard.writeText(link).then(
            () => setCopied(true),
            () => setCopied(false),
          );
        }}
      >
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}

// A name that gives up its owner's link when pressed. The name is the control
// because the link is the only thing there is to do with one person — the answer
// itself belongs to the group.
//
// Copying the link is how a round is distributed until sending is built, it is
// the fallback when an email bounces, and it is how an admin answers on
// somebody's behalf.
//
// It fills a whole wrapping flex row: the name, whatever labels it, and then the
// link on a line of its own. label is passed in rather than rendered after this,
// so that opening the link cannot push the label off the name's line.
function NameLink({
  name,
  link,
  label,
}: {
  name: string;
  link: string;
  label: ReactNode;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        className="name-toggle"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {name}
      </button>
      {label}
      {open && <CopyableLink link={link} />}
    </>
  );
}

// One person inside a group of several. Someone whose partner has answered is
// marked as covered rather than missing: the group has an answer, so chasing
// them would be chasing one we already hold.
function MemberRow({ member }: { member: AvailabilityEntry }) {
  return (
    <li className="member">
      <div className="member-head">
        <NameLink
          name={member.volunteerName}
          link={member.link}
          label={
            <span className="member-note">
              {member.replied
                ? "answered"
                : member.coveredBy.length > 0
                  ? `covered by ${member.coveredBy.join(" and ")}`
                  : "no reply"}
            </span>
          }
        />
      </div>
    </li>
  );
}

// One group's answer over the round's open dates.
//
// The group is the unit: its members are allocated together and one reply speaks
// for all of them, so this is the row an admin chases. The dates are the group
// rule already applied by the server, not a merge done here.
//
// Most volunteers are in no group and are therefore a group of one. Their name
// is the group's name, so it is shown once, in the head, and there is no
// member list under it repeating it.
function GroupRow({
  group,
  shifts,
}: {
  group: AvailabilityGroup;
  shifts: ShiftCoverage[];
}) {
  const available = new Set(group.availableShiftIds);
  const alone = group.members.length === 1 ? group.members[0] : null;
  const tag = group.replied ? (
    <span className="group-tag group-tag--replied">Replied</span>
  ) : (
    <span className="group-tag">No reply</span>
  );

  return (
    <li className="group">
      <div className="group-head">
        {alone ? (
          <NameLink name={group.name} link={alone.link} label={tag} />
        ) : (
          <>
            <span className="group-name">{group.name}</span>
            {tag}
          </>
        )}
      </div>

      {group.replied && (
        <ul className="group-dates">
          {shifts
            .filter((shift) => !shift.closed)
            .map((shift) => {
              const yes = available.has(shift.id);
              return (
                // Colour and the strike carry the answer visually, but neither
                // reaches a screen reader, so the label spells it out — the
                // chips are the answer, not decoration on it.
                <li
                  key={shift.id}
                  className={`group-date${yes ? " group-date--yes" : ""}`}
                  aria-label={`${formatShortDate(shift.date)}: ${
                    yes ? "available" : "not available"
                  }`}
                >
                  {formatShortDate(shift.date)}
                </li>
              );
            })}
        </ul>
      )}

      {!alone && (
        <ul className="members">
          {group.members.map((member) => (
            <MemberRow key={member.volunteerId} member={member} />
          ))}
        </ul>
      )}
    </li>
  );
}

// AdminAvailability is the availability tab: start a round for the latest rota,
// then read whether the answers coming in can actually staff it.
//
// Cover comes first and replies second, because "can I run this rota" is the
// question being asked, and "who still owes me an answer" is only how it gets
// fixed.
export default function AdminAvailability() {
  const { round, error, mintState, mint } = useAvailabilityRound();

  const replied = round?.groups.filter((g) => g.replied).length ?? 0;
  const total = round?.groups.length ?? 0;

  return (
    <section className="admin-panel round">
      <header className="round-head">
        <h2>Availability</h2>
        {round && !round.allocated && (
          <Button
            size="small"
            onClick={() => void mint()}
            disabled={mintState === "minting"}
          >
            {mintState === "minting"
              ? "Starting…"
              : total === 0
                ? "Start round"
                : "Top up round"}
          </Button>
        )}
      </header>

      {error && (
        <p className="round-message round-message--error">
          Could not load the round: {error}
        </p>
      )}

      {round === null && !error && (
        <p className="round-message">Loading the round…</p>
      )}

      {round && (
        <>
          <p className="round-caption">
            Rota {formatRange(round)} · {round.shifts.length} dates
            {round.allocated && " · already allocated, links have closed"}
          </p>

          <h3 className="round-section">Cover</h3>
          <ul className="cover">
            {round.shifts.map((shift) => (
              <CoverageRow key={shift.id} shift={shift} />
            ))}
          </ul>

          {total === 0 ? (
            <p className="round-message">
              Nobody has been asked yet. Starting a round gives every active
              volunteer their own link.
            </p>
          ) : (
            <>
              <h3 className="round-section">
                Replies
                <span className="round-progress">
                  {replied} of {total} groups
                </span>
              </h3>
              <ul className="groups">
                {round.groups.map((group) => (
                  <GroupRow
                    key={group.key}
                    group={group}
                    shifts={round.shifts}
                  />
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </section>
  );
}
