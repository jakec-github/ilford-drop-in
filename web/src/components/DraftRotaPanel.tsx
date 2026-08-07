import type { DraftRotaState } from "../types";
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
function describeOutcome(state: DraftRotaState): string {
  if (!state.success) {
    return (
      "No rota is possible from the availability, pins and shapes as they stand " +
      `(the solver said ${state.solverStatus}).`
    );
  }
  const unfilled = state.seatsAsked - state.seatsFilled;
  if (unfilled <= 0) {
    return `Every seat is filled — all ${state.seatsAsked} of them.`;
  }
  return `${state.seatsFilled} of ${state.seatsAsked} seats filled, ${unfilled} still empty.`;
}

// DraftRotaPanel is what the draft has to say for itself, above the rota it is
// drawn onto: when it was last solved, how much of the rota it staffed, whether
// a rota was possible at all, and the control that solves it again.
//
// Admin-only, like everything else about a draft. Its caller renders it only
// when a rota is in flight — there is otherwise nothing to draft, and a panel
// saying so on the public rota page would be a permanent empty box.
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
  return (
    <section className="draft-panel">
      <div className="draft-panel-head">
        <h2 className="draft-panel-title">Draft rota</h2>
        <Button size="small" onClick={onSolve} disabled={solving}>
          {solving ? "Solving…" : state.solved ? "Solve again" : "Solve now"}
        </Button>
      </div>

      {state.solved && state.solvedAt !== null ? (
        <p className="draft-panel-state">
          {describeOutcome(state)}{" "}
          {/* The instant in the title, so the exact time is a hover away
              without putting a timestamp nobody reads in the sentence. */}
          <span className="draft-panel-when" title={state.solvedAt}>
            Solved {timeAgo(state.solvedAt)}.
          </span>
        </p>
      ) : (
        <p className="draft-panel-state">
          Nothing has been solved for this rota yet.
        </p>
      )}

      {/* A solve was already running when this page read the draft, so what is
          above is the previous answer. Worth saying rather than leaving an
          admin to wonder why a re-solve changed nothing: the answer they want
          is being computed, and reloading is what collects it. */}
      {state.solving && (
        <p className="draft-panel-state">
          A fresher draft is being solved right now — reload in a moment to see
          it.
        </p>
      )}

      {/* Said on every state, because it is true on every state and it is the
          thing most easily forgotten: a draft is a guess, and it is a guess
          about inputs that keep moving. */}
      <p className="draft-panel-note">
        A draft is what the solver makes of the availability, pins and shapes as
        they stood when it ran &mdash; not the rota, until it is allocated. It
        is solved again whenever one of those moves, so opening this page is
        usually enough to bring it up to date. To hold somebody to a shift, pin
        them.
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
