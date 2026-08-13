import { useState } from "react";
import Button from "../ui/Button";
import Dialog from "../ui/Dialog";
import ResponseGrid from "./ResponseGrid";
import { useAvailabilityRound } from "../hooks/useAvailabilityRound";
import { useAvailabilitySend } from "../hooks/useAvailabilitySend";
import type { AvailabilityRound, AvailabilitySend, SendMode } from "../types";
import "./AvailabilityPanel.css";

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

// AvailabilityPanel is the asking half of the Allocation tab: start a round for
// the rota in flight, send everyone their link, then read whether the answers
// coming back can actually staff it.
//
// This component owns the round and the sending; reading it belongs to the grid.
// The two questions this used to answer in two lists — "is this rota covered"
// and "who still owes me an answer" — are one matrix now, because they are the
// same question read in two directions.
//
// A section rather than a tab of its own (issue #145). Asking is not a step to
// be finished before the next one starts: the shifts keep being edited while the
// answers come in, and the draft is re-solved as they land, so the round belongs
// on the screen those are on.
export default function AvailabilityPanel() {
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
    // The panel is wrapped rather than laid out in place: the grid inside it is
    // as wide as the rota is long, so this is the one panel on the tab that has
    // to be able to leave the admin column. What that takes is a box wider than
    // the column to centre it in — see the CSS (issue #174).
    <div className="round-bleed">
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

            {total === 0 ? (
              // Rare now: a rota opens its round as it is defined. What is left
              // here is the rota whose roster read failed at that moment, so the
              // message says how to finish the job rather than describing a step.
              <p className="round-message">
                Nobody has been asked yet — a rota is usually given its links as
                it is defined, so the roster could not be read then. Starting a
                round gives every active volunteer their own link.
              </p>
            ) : (
              <>
                <h3 className="round-section">
                  Responses
                  <span className="round-progress">
                    {replied} of {total} groups replied
                  </span>
                </h3>
                <ResponseGrid
                  round={round}
                  onResend={(member) =>
                    setPending({
                      mode: "resend",
                      volunteerId: member.volunteerId,
                      volunteerName: member.volunteerName,
                    })
                  }
                />
              </>
            )}
          </>
        )}
      </section>
    </div>
  );
}
