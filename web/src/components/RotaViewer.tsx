import { useMemo, useState } from "react";
import type { Rota, RotaShift } from "../types";
import "./RotaViewer.css";

interface RotaViewerProps {
  rota: Rota;
}

function formatBritishDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

function formatStartDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString("en-GB");
}

function getNextShiftDate(shifts: RotaShift[]): string | null {
  const today = new Date();
  today.setHours(0, 0, 0, 0);

  for (const shift of shifts) {
    const shiftDate = new Date(shift.date);
    if (shiftDate >= today) {
      return shift.date;
    }
  }
  return null;
}

function getAllNames(rota: Rota): string[] {
  const names = new Set<string>();
  for (const shift of rota.shifts) {
    if (shift.teamLead && shift.teamLead !== "CLOSED") {
      names.add(shift.teamLead);
    }
    for (const vol of shift.volunteers) {
      names.add(vol);
    }
  }
  return Array.from(names).sort();
}

function ShiftCard({
  shift,
  isHighlighted,
}: {
  shift: RotaShift;
  isHighlighted: boolean;
}) {
  const isClosed = shift.teamLead === "CLOSED";

  return (
    <div
      className={`shift-card ${isClosed ? "closed" : ""} ${isHighlighted ? "highlighted" : ""}`}
    >
      <div className="shift-date">{formatBritishDate(shift.date)}</div>
      <div className="shift-team-lead">
        {isClosed ? (
          <span className="shift-closed-label">Closed</span>
        ) : (
          <strong>{shift.teamLead}</strong>
        )}
      </div>
      {!isClosed && (
        <div className="shift-volunteers">
          {shift.volunteers.map((vol, i) => (
            <span key={i} className="volunteer-name">
              {vol}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

export default function RotaViewer({ rota }: RotaViewerProps) {
  const [selectedName, setSelectedName] = useState("");
  const allNames = useMemo(() => getAllNames(rota), [rota]);
  const nextShiftDate = useMemo(() => getNextShiftDate(rota.shifts), [rota]);

  const filteredShifts = selectedName
    ? rota.shifts.filter(
        (shift) =>
          shift.teamLead === selectedName ||
          shift.volunteers.includes(selectedName),
      )
    : rota.shifts;

  return (
    <div className="rota-viewer">
      <h1>Ilford Drop-in Rota</h1>
      <p className="rota-subtitle">
        {rota.shiftCount} shifts from {formatStartDate(rota.startDate)}
      </p>

      <div className="rota-filter">
        <select
          value={selectedName}
          onChange={(e) => setSelectedName(e.target.value)}
          className="name-select"
        >
          <option value="">All volunteers</option>
          {allNames.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </div>

      <div className="rota-table">
        <div className="rota-table-header">
          <div className="header-row">
            <th>Date</th>
            <th>Team Lead</th>
            <th>Volunteers</th>
          </div>
        </div>
        <div className="rota-table-body">
          {filteredShifts.map((shift, i) => (
            <ShiftCard
              key={i}
              shift={shift}
              isHighlighted={shift.date === nextShiftDate}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
