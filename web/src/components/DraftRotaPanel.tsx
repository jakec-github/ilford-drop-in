import type { DraftRotaState, SolvedDraft } from "../types";
import Button from "../ui/Button";
import "./DraftRotaPanel.css";

// How long ago the solve ran, which is the only thing anybody asks of a draft's
// timestamp: the inputs move under it all through the availability window, so
// "six hours ago" answers "is this worth trusting" where a clock time does not.
//
// Rounded down to the coarsest unit that still says something, and never
// negative — a clock a few seconds behind the server's must not produce "in 4
// seconds".
function timeAgo(iso: string): string {
  const solved = new Date(iso).getTime();
  if (Number.isNaN(solved)) return "at an unknown time";

  const minutes = Math.max(0, Math.floor((Date.now() - solved) / 60000));
  if (minutes < 1) return "just now";
  if (minutes < 60)
    return `${minutes} ${minutes === 1 ? "minute" : "minutes"} ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} ${hours === 1 ? "hour" : "hours"} ago`;

  const days = Math.floor(hours / 24);
  return `${days} ${days === 1 ? "day" : "days"} ago`;
}

// What the solve concluded, in the terms an admin acts on.
//
// The two outcomes lead to different work, so they are worded as different
// sentences rather than as a status with a number beside it. Unfilled Seats are
// people to chase; an infeasible solve is a conflict in the inputs to go and
// resolve, and no amount of chasing fixes it.
function describeOutcome(draft: SolvedDraft, seatsAsked: number): string {
  if (!draft.success) {
    return (
      "No rota is possible from the availability, pins and shapes as they stand " +
      `(the solver said ${draft.solverStatus}).`
    );
  }
  const unfilled = seatsAsked - draft.seatsFilled;
  if (unfilled <= 0) {
    return `Every seat is filled — all ${seatsAsked} of them.`;
  }
  return `${draft.seatsFilled} of ${seatsAsked} seats filled, ${unfilled} still empty.`;
}

// DraftRotaPanel is what the draft has to say for itself, above the rota it is
// drawn onto: when it was last solved, how much of the rota it staffed, whether
// a rota was possible at all, and the control that solves it again.
//
// Admin-only, like everything else about a draft. It renders nothing at all when
// no rota is in flight — there is then nothing to draft, and a panel saying so
// on the public rota page would be a permanent empty box.
export default function DraftRotaPanel({
  state,
  solving,
  solveError,
  onSolve,
}: {
  state: DraftRotaState;
  solving: boolean;
  solveError: string | null;
  onSolve: () => void;
}) {
  if (state.rota === null) return null;
  const { draft } = state;

  return (
    <section className="draft-panel">
      <div className="draft-panel-head">
        <h2 className="draft-panel-title">Draft rota</h2>
        <Button size="small" onClick={onSolve} disabled={solving}>
          {solving ? "Solving…" : draft ? "Solve again" : "Solve now"}
        </Button>
      </div>

      {draft === null ? (
        <p className="draft-panel-state">
          Nothing has been solved for this rota yet.{" "}
          {state.rota.seatsAsked > 0
            ? `It is asking for ${state.rota.seatsAsked} seats.`
            : "Its shifts are not asking for anybody yet."}
        </p>
      ) : (
        <p className="draft-panel-state">
          {describeOutcome(draft, state.rota.seatsAsked)}{" "}
          {/* The instant in the title, so the exact time is a hover away
              without putting a timestamp nobody reads in the sentence. */}
          <span className="draft-panel-when" title={draft.solvedAt}>
            Solved {timeAgo(draft.solvedAt)}.
          </span>
        </p>
      )}

      {/* Said on every state, because it is true on every state and it is the
          thing most easily forgotten: a draft is a guess, and it is a guess
          about inputs that keep moving. */}
      <p className="draft-panel-note">
        A draft is what the solver makes of the availability, pins and shapes as
        they stood when it ran &mdash; not the rota, until it is allocated. It
        is solved again every few hours, and solving now takes in anything that
        has changed since. To hold somebody to a shift, pin them.
      </p>

      {/* A refused solve names the step that has not been taken — an
          availability round nobody has minted, settings nobody has filled in —
          so it is shown verbatim rather than summarised. */}
      {solveError && (
        <p className="draft-panel-error" role="alert">
          {solveError}
        </p>
      )}
    </section>
  );
}
