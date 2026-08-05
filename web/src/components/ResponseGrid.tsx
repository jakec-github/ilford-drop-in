import { useState, type ReactNode } from "react";
import Button from "../ui/Button";
import type {
  AvailabilityEntry,
  AvailabilityGroup,
  AvailabilityRound,
  Role,
  ShiftCoverage,
} from "../types";
import "./ResponseGrid.css";

// The grid is a matrix because that is the shape of the question. "Can I staff
// this rota" is read down a column, "who still owes me an answer" is read along
// a row, and both were previously two lists that had to be held side by side in
// the reader's head.
//
// The Role deltas sit at the top of the same columns as the answers, so a
// negative number and the empty cells that caused it line up under one date.

// The date, over two lines: a shift is named by its weekday as much as its
// number, and a column narrow enough to fit six of them cannot take both on one.
function shiftWeekday(date: string): string {
  return new Date(date).toLocaleDateString("en-GB", { weekday: "short" });
}

function shiftDay(date: string): string {
  return new Date(date).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
  });
}

function fullDate(date: string): string {
  return new Date(date).toLocaleDateString("en-GB", {
    weekday: "long",
    day: "numeric",
    month: "long",
  });
}

// Every configured Role, in the priority order the server sends. Taken from the
// shifts rather than named here: which Roles exist is configuration, and the
// coverage the API already returns per shift is the authority on it. Closed
// shifts carry none, hence the sweep over all of them.
function roleNames(shifts: ShiftCoverage[]): Role[] {
  const names: Role[] = [];
  for (const shift of shifts) {
    for (const coverage of shift.roles) {
      if (!names.includes(coverage.role)) names.push(coverage.role);
    }
  }
  return names;
}

// A group is offered for a Role if anybody in it holds that Role — the filter
// asks "could this row help with leads", and a pair containing one lead can.
function holdsRole(group: AvailabilityGroup, role: Role): boolean {
  return group.members.some((member) => member.roles.includes(role));
}

// Where one person has got to, in the order the states matter. An answer settles
// it. Failing that, a partner's answer covers them — the group has an answer, so
// chasing them would be chasing one we already hold. Only then is silence worth
// reporting, and it reads differently depending on whether they were ever
// emailed: nobody has failed to reply to a link that was never sent.
function memberNote(member: AvailabilityEntry): string {
  if (member.replied) return "answered";
  if (member.coveredBy.length > 0) {
    return `covered by ${member.coveredBy.join(" and ")}`;
  }
  return member.sentAt === null ? "not sent" : "no reply";
}

// The URL, the button that copies it, and whatever else can be done with one
// person's link. The URL is shown in full as well as copied: copying fails
// silently on an insecure origin or a locked-down browser, and an admin who can
// read it can always select it by hand.
function CopyableLink({
  link,
  actions,
}: {
  link: string;
  actions?: ReactNode;
}) {
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
      {actions}
    </div>
  );
}

// Everything there is to do with one person, behind the row's disclosure: open
// their form, copy their link, mail it to them again.
//
// Opening the form is how an admin answers on somebody's behalf — the phone call
// that ends "put me down for the 14th" — so it is a real link to the real page a
// volunteer sees, in a new tab. An allocated round has no working links left, so
// it drops to plain text rather than offering a 410.
function MemberDetails({
  member,
  allocated,
  named,
  onResend,
}: {
  member: AvailabilityEntry;
  allocated: boolean;
  named: boolean;
  onResend: (member: AvailabilityEntry) => void;
}) {
  return (
    <li className="member">
      {named && (
        <div className="member-head">
          {allocated ? (
            <span className="member-name">{member.volunteerName}</span>
          ) : (
            <a
              className="member-name"
              href={member.link}
              target="_blank"
              rel="noreferrer"
            >
              {member.volunteerName}
            </a>
          )}
          <span className="member-note">{memberNote(member)}</span>
        </div>
      )}
      {!allocated && (
        <CopyableLink
          link={member.link}
          actions={
            <Button size="small" onClick={() => onResend(member)}>
              {member.sentAt === null ? "Send" : "Resend"}
            </Button>
          }
        />
      )}
    </li>
  );
}

// One Role's surplus or deficit on one shift.
//
// The signed delta is the number being scanned for, so it is the only thing in
// the cell; the arithmetic behind it — how many are needed, how many could come —
// is on the cell's own label, where a reader who wants it can hover or hear it.
// Zero stays neutral: a shift that is exactly full is fine, and colouring it too
// would leave nothing standing out.
function DeltaCell({ shift, role }: { shift: ShiftCoverage; role: Role }) {
  if (shift.closed) {
    return (
      <td className="grid-cell grid-cell--closed">
        <span aria-hidden="true">—</span>
        <span className="grid-hidden">closed</span>
      </td>
    );
  }

  const coverage = shift.roles.find((r) => r.role === role);
  if (coverage === undefined) {
    return <td className="grid-cell">—</td>;
  }

  const tone =
    coverage.delta < 0
      ? " grid-num--short"
      : coverage.delta > 0
        ? " grid-num--ok"
        : "";

  return (
    <td className="grid-cell">
      <span
        className={`grid-num${tone}`}
        title={`${coverage.available} available, ${coverage.needed} needed`}
      >
        {/* The signed number is the visual shorthand for the sentence beside
            it, so it is hidden rather than read out as a bare "-1". */}
        <span aria-hidden="true">
          {coverage.delta > 0 ? `+${coverage.delta}` : coverage.delta}
        </span>
        <span className="grid-hidden">
          {`${coverage.available} available of ${coverage.needed} needed`}
        </span>
      </span>
    </td>
  );
}

// One group's answer for one shift.
//
// Three states, not two: available, not available, and not answered. The third
// is a blank cell rather than a cross, because a group that has said nothing has
// not said no — treating silence as a refusal is the mistake the whole round
// exists to avoid. Colour and the tick carry it visually; neither reaches a
// screen reader, so each cell also spells its answer out.
function AnswerCell({
  group,
  shift,
}: {
  group: AvailabilityGroup;
  shift: ShiftCoverage;
}) {
  if (shift.closed) {
    return (
      <td className="grid-cell grid-cell--closed">
        <span className="grid-hidden">closed</span>
      </td>
    );
  }
  if (!group.replied) {
    return (
      <td className="grid-cell">
        <span className="grid-hidden">no reply</span>
      </td>
    );
  }

  const yes = group.availableShiftIds.includes(shift.id);
  return (
    <td className={`grid-cell${yes ? " grid-cell--yes" : ""}`}>
      <span aria-hidden="true">{yes ? "✓" : "·"}</span>
      <span className="grid-hidden">{yes ? "available" : "not available"}</span>
    </td>
  );
}

// One group's row: who they are, and their answer across the dates.
//
// The group is the unit, not the volunteer: its members are allocated together
// and one reply speaks for all of them, so it is the row an admin chases and the
// row the answer belongs to. Members appear inside the disclosure, where their
// individual links live.
function GroupRow({
  group,
  shifts,
  allocated,
  open,
  onToggle,
  onResend,
}: {
  group: AvailabilityGroup;
  shifts: ShiftCoverage[];
  allocated: boolean;
  open: boolean;
  onToggle: () => void;
  onResend: (member: AvailabilityEntry) => void;
}) {
  const alone = group.members.length === 1 ? group.members[0] : null;
  // A group nobody has emailed is not a group that has failed to reply. On a
  // freshly minted round that is every row, and calling them all "no reply"
  // would report a problem that is really just the send not having happened.
  const unsent = group.members.every((m) => m.sentAt === null);

  return (
    <>
      <tr className={open ? "grid-row grid-row--open" : "grid-row"}>
        <th scope="row" className="grid-name-cell">
          <button
            type="button"
            className="grid-toggle"
            aria-expanded={open}
            aria-label={`Links for ${group.name}`}
            onClick={onToggle}
          >
            <span aria-hidden="true">{open ? "▾" : "▸"}</span>
          </button>
          {alone !== null && !allocated ? (
            <a
              className="grid-name"
              href={alone.link}
              target="_blank"
              rel="noreferrer"
            >
              {group.name}
            </a>
          ) : (
            <span className="grid-name">{group.name}</span>
          )}
          {/* A real space, so the row header does not announce as
              "Abigail WhiteReplied" — the margin between them is only paint. */}{" "}
          {group.replied ? (
            <span className="grid-tag grid-tag--replied">Replied</span>
          ) : (
            <span className="grid-tag">{unsent ? "Not sent" : "No reply"}</span>
          )}
        </th>
        {shifts.map((shift) => (
          <AnswerCell key={shift.id} group={group} shift={shift} />
        ))}
      </tr>

      {open && (
        <tr className="grid-details">
          <td colSpan={shifts.length + 1}>
            <ul className="members">
              {group.members.map((member) => (
                <MemberDetails
                  key={member.volunteerId}
                  member={member}
                  allocated={allocated}
                  // A group of one is already named by its row, and the link
                  // under it can only be theirs.
                  named={alone === null}
                  onResend={onResend}
                />
              ))}
            </ul>
          </td>
        </tr>
      )}
    </>
  );
}

// ResponseGrid is the round as a matrix: groups down the side, the rota's shifts
// along the top, and each Role's surplus or deficit in the same columns above
// the answers that produced it.
//
// It is deliberately wider than a phone. An admin reading a round is comparing
// dates against each other, which is what the grid is for, and squeezing that
// into one column would cost the comparison to save a scroll.
export default function ResponseGrid({
  round,
  onResend,
}: {
  round: AvailabilityRound;
  onResend: (member: AvailabilityEntry) => void;
}) {
  const [filter, setFilter] = useState<Role | null>(null);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());

  const roles = roleNames(round.shifts);
  const groups =
    filter === null
      ? round.groups
      : round.groups.filter((group) => holdsRole(group, filter));

  // Pins are why a date can want fewer people than the config says, so they are
  // worth a row — but only on a round that has any, where the row explains
  // something rather than repeating a column of zeroes.
  const pinned = round.shifts.map((shift) =>
    shift.roles.reduce((total, role) => total + role.pinned, 0),
  );
  const anyPinned = pinned.some((count) => count > 0);

  const toggle = (key: string) =>
    setOpen((current) => {
      const next = new Set(current);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  return (
    <>
      {/* Filtering answers "who could lead this date", which is the question a
          red Team lead delta raises. It narrows the rows only: the deltas above
          are the whole shift's position and do not change because a reader is
          looking at part of the roster. */}
      {roles.length > 1 && (
        <div className="grid-filter" role="group" aria-label="Filter by role">
          <button
            type="button"
            className={
              filter === null ? "grid-chip grid-chip--on" : "grid-chip"
            }
            aria-pressed={filter === null}
            onClick={() => setFilter(null)}
          >
            Everyone
          </button>
          {roles.map((role) => (
            <button
              key={role}
              type="button"
              className={
                filter === role ? "grid-chip grid-chip--on" : "grid-chip"
              }
              aria-pressed={filter === role}
              onClick={() => setFilter(filter === role ? null : role)}
            >
              {role}
            </button>
          ))}
        </div>
      )}

      {/* The table scrolls inside this rather than the page: the first column is
          pinned, so a reader who has scrolled to the last date can still see
          whose row they are on. */}
      <div className="grid-scroll">
        <table className="response-grid">
          <caption className="grid-hidden">
            Availability by group, with each Role&rsquo;s surplus or deficit per
            shift
          </caption>
          <thead>
            <tr>
              <th scope="col" className="grid-name-cell">
                Group
              </th>
              {round.shifts.map((shift) => (
                <th
                  key={shift.id}
                  scope="col"
                  className={
                    shift.closed ? "grid-date grid-date--closed" : "grid-date"
                  }
                >
                  <span aria-hidden="true">{shiftWeekday(shift.date)}</span>
                  <span aria-hidden="true" className="grid-date-day">
                    {shiftDay(shift.date)}
                  </span>
                  <span className="grid-hidden">
                    {fullDate(shift.date)}
                    {shift.closed && " (closed)"}
                  </span>
                  {shift.closed && (
                    <span aria-hidden="true" className="grid-date-note">
                      closed
                    </span>
                  )}
                </th>
              ))}
            </tr>
          </thead>

          <tbody className="grid-stats">
            {roles.map((role) => (
              <tr key={role}>
                <th scope="row" className="grid-name-cell">
                  {role}
                </th>
                {round.shifts.map((shift) => (
                  <DeltaCell key={shift.id} shift={shift} role={role} />
                ))}
              </tr>
            ))}
            {anyPinned && (
              <tr>
                <th scope="row" className="grid-name-cell">
                  Already pinned
                </th>
                {round.shifts.map((shift, i) => (
                  <td key={shift.id} className="grid-cell">
                    {shift.closed ? "—" : pinned[i] || ""}
                  </td>
                ))}
              </tr>
            )}
          </tbody>

          <tbody>
            {groups.map((group) => (
              <GroupRow
                key={group.key}
                group={group}
                shifts={round.shifts}
                allocated={round.allocated}
                open={open.has(group.key)}
                onToggle={() => toggle(group.key)}
                onResend={onResend}
              />
            ))}
          </tbody>
        </table>
      </div>

      {groups.length === 0 && (
        <p className="round-message">Nobody in this round holds that role.</p>
      )}
    </>
  );
}
