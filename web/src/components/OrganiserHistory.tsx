import { useVolunteerHistory } from "../hooks/useVolunteerHistory";
import HistoryGrid from "./HistoryGrid";

// OrganiserHistory is the History tab: how each volunteer has answered, been
// given and turned up for the last allocated rotas (issue #260).
export default function OrganiserHistory() {
  const { history, error } = useVolunteerHistory();

  return (
    <section className="organiser-panel">
      <h2>History</h2>
      <p>
        Availability is read as it stood when each rota was allocated: low is
        under a quarter of its open shifts, ok up to half, good over half.
        Attended is the rota after any swaps or covers.
      </p>
      {error && <p role="alert">Could not load the history: {error}</p>}
      {!error && history === null && <p>Loading…</p>}
      {history &&
        (history.rotations.length === 0 ? (
          <p>No rota has been allocated yet.</p>
        ) : (
          <HistoryGrid history={history} />
        ))}
    </section>
  );
}
