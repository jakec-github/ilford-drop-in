import { useState, type ReactNode } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import { useAvailabilityRound } from "../hooks/useAvailabilityRound";
import { useAvailabilitySend } from "../hooks/useAvailabilitySend";
import type {
  AvailabilityEntry,
  AvailabilityGroup,
  AvailabilityRound,
  AvailabilitySend,
  SendMode,
  ShiftCoverage,
} from "../types";
import "./AdminAvailability.css";

// A send the admin has asked for but not yet given a deadline to. The deadline
// is the one thing a send cannot be started without, so it is what the dialog
// exists to collect.
interface PendingSend {
  mode: SendMode;
  volunteerId?: string;
  volunteerName?: string;
}

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
        {/* A capped Role with Seats and nobody to fill them is short in a way
            the delta cannot express: no number of ordinary volunteers gets a
            shift a lead. The uncapped Role is left to the delta, which is the
            same fact stated better. */}
        {shift.roles
          .filter((r) => r.capped && r.needed > 0 && r.available === 0)
          .map((r) => (
            <span key={r.role} className="cover-tag cover-tag--short">
              No {r.role.toLowerCase()}
            </span>
          ))}
      </span>
    </li>
  );
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
  actions,
}: {
  name: string;
  link: string;
  label: ReactNode;
  actions?: ReactNode;
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
      {open && <CopyableLink link={link} actions={actions} />}
    </>
  );
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

// A button that mails one person their link again, for the email that bounced,
// went to spam, or was deleted. It sits behind the same disclosure as the link
// itself, because it is the other thing there is to do with one person.
function ResendButton({
  member,
  onResend,
}: {
  member: AvailabilityEntry;
  onResend: (member: AvailabilityEntry) => void;
}) {
  return (
    <Button size="small" onClick={() => onResend(member)}>
      {member.sentAt === null ? "Send" : "Resend"}
    </Button>
  );
}

// One person inside a group of several.
function MemberRow({
  member,
  onResend,
}: {
  member: AvailabilityEntry;
  onResend: (member: AvailabilityEntry) => void;
}) {
  return (
    <li className="member">
      <div className="member-head">
        <NameLink
          name={member.volunteerName}
          link={member.link}
          label={<span className="member-note">{memberNote(member)}</span>}
          actions={<ResendButton member={member} onResend={onResend} />}
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
  onResend,
}: {
  group: AvailabilityGroup;
  shifts: ShiftCoverage[];
  onResend: (member: AvailabilityEntry) => void;
}) {
  const available = new Set(group.availableShiftIds);
  const alone = group.members.length === 1 ? group.members[0] : null;
  // A group nobody has emailed is not a group that has failed to reply. On a
  // freshly minted round that is every row, and calling them all "no reply"
  // would report a problem that is really just the send not having happened.
  const unsent = group.members.every((m) => m.sentAt === null);
  const tag = group.replied ? (
    <span className="group-tag group-tag--replied">Replied</span>
  ) : (
    <span className="group-tag">{unsent ? "Not sent" : "No reply"}</span>
  );

  return (
    <li className="group">
      <div className="group-head">
        {alone ? (
          <NameLink
            name={group.name}
            link={alone.link}
            label={tag}
            actions={<ResendButton member={alone} onResend={onResend} />}
          />
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
            <MemberRow
              key={member.volunteerId}
              member={member}
              onResend={onResend}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

// The deadline, asked for once per send.
//
// It is the only thing a send needs that the round does not already know, and it
// is asked for every time rather than remembered because it is a different date
// each round. Nothing stores it: it is quoted in the email and nowhere else, and
// the real cutoff is allocation.
//
// Submitting navigates the whole page out to Google, so there is no pending
// state to show — the dialog is gone by the time anything has happened.
function SendDialog({
  pending,
  onSend,
  onClose,
}: {
  pending: PendingSend;
  onSend: (deadline: string) => void;
  onClose: () => void;
}) {
  const [deadline, setDeadline] = useState("");

  const title =
    pending.mode === "reminder"
      ? "Send reminders"
      : pending.volunteerName
        ? `Send to ${pending.volunteerName}`
        : "Send the round";

  return (
    <Dialog title={title} onClose={onClose}>
      <form
        className="send-form"
        onSubmit={(e) => {
          e.preventDefault();
          onSend(deadline.trim());
        }}
      >
        <label className="send-field">
          Deadline
          <input
            value={deadline}
            onChange={(e) => setDeadline(e.target.value)}
            placeholder="Friday 7 August"
            autoFocus
          />
        </label>
        <p className="send-note">
          Quoted in the email as the date to answer by. It is not shown on the
          site and nothing enforces it — answers keep counting until the rota is
          allocated.
        </p>
        <p className="send-note">
          Mail sends from your own Google account, so Google will ask you to
          allow it the first time.
        </p>
        <div className="send-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={deadline.trim() === ""}>
            Send
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// What a send did, or is doing.
//
// A running send counts up rather than spinning: it takes about ninety seconds,
// and a bar with no numbers on it for that long is indistinguishable from a
// hang. A finished one leads with its failures, because those are the ones that
// still need an admin — the successes are only there to say how many there were.
function SendReport({
  send,
  onDismiss,
}: {
  send: AvailabilitySend;
  onDismiss: () => void;
}) {
  const noun = send.mode === "reminder" ? "reminder" : "email";
  const sent = `Sent ${send.sent.length} ${noun}${send.sent.length === 1 ? "" : "s"}`;

  return (
    <div className="send-report">
      <div className="send-report-head">
        {send.finished ? (
          <span>
            {sent}
            {send.failed.length > 0 && `, ${send.failed.length} failed`}
          </span>
        ) : (
          <span>
            Sending… {send.done} of {send.total}
          </span>
        )}
        {send.finished && (
          <Button size="small" onClick={onDismiss}>
            Dismiss
          </Button>
        )}
      </div>

      {send.error !== null && <p className="send-report-error">{send.error}</p>}

      {send.failed.length > 0 && (
        <ul className="send-failures">
          {send.failed.map((f) => (
            <li key={f.volunteerId}>
              <strong>{f.volunteerName}</strong>
              {f.email !== null && ` (${f.email})`} — {f.error}
            </li>
          ))}
        </ul>
      )}

      {send.finished && send.failed.length > 0 && (
        <p className="send-note">
          Nobody here has been marked as sent, so sending the round again will
          try them and leave everyone else alone.
        </p>
      )}
    </div>
  );
}

// AdminAvailability is the availability tab: start a round for the latest rota,
// send everyone their link, then read whether the answers coming back can
// actually staff it.
//
// Cover comes first and replies second, because "can I run this rota" is the
// question being asked, and "who still owes me an answer" is only how it gets
// fixed.
export default function AdminAvailability() {
  const { round, error, mintState, mint, reload } = useAvailabilityRound();
  const {
    send,
    error: sendError,
    start,
    dismiss,
  } = useAvailabilitySend(reload);
  const [pending, setPending] = useState<PendingSend | null>(null);

  const replied = round?.groups.filter((g) => g.replied).length ?? 0;
  const total = round?.groups.length ?? 0;

  const members = round?.groups.flatMap((g) => g.members) ?? [];
  // What the two round-level sends would actually do, so each button can say so
  // and disable itself when it would do nothing.
  const unsent = members.filter((m) => m.sentAt === null).length;
  const toChase = round
    ? round.groups.filter(
        (g) => !g.replied && g.members.some((m) => m.sentAt !== null),
      ).length
    : 0;

  const startSend = (deadline: string) => {
    if (pending === null) return;
    start(pending.mode, deadline, pending.volunteerId);
  };

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

      {sendError !== null && (
        <p className="round-message round-message--error">{sendError}</p>
      )}

      {send !== null && <SendReport send={send} onDismiss={dismiss} />}

      {pending !== null && (
        <SendDialog
          pending={pending}
          onSend={startSend}
          onClose={() => setPending(null)}
        />
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

          {/* Sending is separate from minting on purpose: minting writes the
              links and needs no Google access, sending is a repeatable action
              over links that already exist. An allocated round has none worth
              sending, so the actions go with it. */}
          {!round.allocated && total > 0 && (
            <div className="round-actions">
              <Button
                size="small"
                disabled={unsent === 0}
                onClick={() => setPending({ mode: "round" })}
              >
                {unsent === 0 ? "All sent" : `Send round (${unsent})`}
              </Button>
              <Button
                size="small"
                disabled={toChase === 0}
                onClick={() => setPending({ mode: "reminder" })}
              >
                {toChase === 0
                  ? "Nobody to chase"
                  : `Send reminders (${toChase})`}
              </Button>
            </div>
          )}

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
                    onResend={(member) =>
                      setPending({
                        mode: "resend",
                        volunteerId: member.volunteerId,
                        volunteerName: member.volunteerName,
                      })
                    }
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
