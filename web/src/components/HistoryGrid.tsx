import { useMemo, useState } from "react";
import type {
  AvailabilityBand,
  HistoryEntry,
  HistoryVolunteer,
  VolunteerHistory,
} from "../types";
import "./HistoryGrid.css";

type Mode = "availability" | "given" | "attended";
type Sort = "name" | "availability" | "attendance";

const MODES: { value: Mode; label: string }[] = [
  { value: "availability", label: "Availability" },
  { value: "given", label: "Shifts given" },
  { value: "attended", label: "Shifts attended" },
];

const BAND_LABEL: Record<AvailabilityBand, string> = {
  not_sent: "Not sent",
  no_response: "Not responded",
  unavailable: "Unavailable",
  low: "Low",
  ok: "Ok",
  good: "Good",
};

function rotaLabel(start: string): string {
  return new Date(start).toLocaleDateString("en-GB", {
    month: "short",
    year: "numeric",
  });
}

// offeredShare is how much of the rotas they were asked about a volunteer
// offered, as one fraction. A rota never sent to them says nothing about them,
// so it is left out rather than counted as nothing offered; silence is counted
// as nothing, because it is what an Organiser had to plan around.
function offeredShare(volunteer: HistoryVolunteer): number {
  let offered = 0;
  let open = 0;
  for (const e of volunteer.history) {
    if (e.availability === "not_sent") continue;
    offered += e.available;
    open += e.openShifts;
  }
  return open === 0 ? Infinity : offered / open;
}

function attendedTotal(volunteer: HistoryVolunteer): number {
  return volunteer.history.reduce((sum, e) => sum + e.attended, 0);
}

function cell(mode: Mode, e: HistoryEntry) {
  if (mode === "availability") {
    const title =
      e.availability === "low" ||
      e.availability === "ok" ||
      e.availability === "good"
        ? `${e.available} of ${e.openShifts} shifts`
        : undefined;
    return (
      <span
        className={`history-band history-band--${e.availability}`}
        title={title}
      >
        {BAND_LABEL[e.availability]}
      </span>
    );
  }
  return mode === "given" ? e.given : e.attended;
}

// HistoryGrid is the History tab's table: a volunteer per row, a past rota per
// column, showing one of three measures at a time.
export default function HistoryGrid({
  history,
}: {
  history: VolunteerHistory;
}) {
  const [mode, setMode] = useState<Mode>("availability");
  const [sort, setSort] = useState<Sort>("name");

  const rows = useMemo(() => {
    const byName = (a: HistoryVolunteer, b: HistoryVolunteer) =>
      a.fullName.localeCompare(b.fullName);
    const sorted = [...history.volunteers];
    if (sort === "availability") {
      sorted.sort((a, b) => offeredShare(a) - offeredShare(b) || byName(a, b));
    } else if (sort === "attendance") {
      sorted.sort(
        (a, b) => attendedTotal(a) - attendedTotal(b) || byName(a, b),
      );
    } else {
      sorted.sort(byName);
    }
    return sorted;
  }, [history.volunteers, sort]);

  return (
    <div className="history">
      <div className="history-controls">
        <div className="history-modes" role="radiogroup" aria-label="Show">
          {MODES.map((m) => (
            <label key={m.value} className="history-mode">
              <input
                type="radio"
                name="history-mode"
                value={m.value}
                checked={mode === m.value}
                onChange={() => setMode(m.value)}
              />
              {m.label}
            </label>
          ))}
        </div>
        <label className="history-sort">
          Sort by
          <select
            value={sort}
            onChange={(e) => setSort(e.target.value as Sort)}
          >
            <option value="name">Name</option>
            <option value="availability">Lowest availability</option>
            <option value="attendance">Lowest attendance</option>
          </select>
        </label>
      </div>
      <div className="history-scroll">
        <table className="history-grid">
          <thead>
            <tr>
              <th scope="col">Volunteer</th>
              {history.rotations.map((r) => (
                <th key={r.id} scope="col">
                  {rotaLabel(r.start)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((v) => (
              <tr
                key={v.id}
                className={v.active ? undefined : "history-row--inactive"}
              >
                <th scope="row">{v.fullName}</th>
                {v.history.map((e, i) => (
                  <td key={history.rotations[i]?.id ?? i}>{cell(mode, e)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
