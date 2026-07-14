import { useMemo, useState, useRef, useEffect, useCallback } from "react";
import type { RotaShift } from "../types";
import "./RotaViewer.css";

interface RotaViewerProps {
  rotaShifts: RotaShift[];
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

function getAllNames(shifts: RotaShift[]): string[] {
  const names = new Set<string>();
  for (const shift of shifts) {
    if (shift.teamLead && shift.teamLead !== "CLOSED") names.add(shift.teamLead);
    for (const vol of shift.volunteers) names.add(vol);
  }
  return Array.from(names).sort();
}

function getUpcomingShifts(shifts: RotaShift[], name: string): RotaShift[] {
  if (!name) return [];
  return shifts.filter(
    (shift) => shift.teamLead === name || shift.volunteers.includes(name),
  );
}

function ShiftCard({
  shift,
  selectedName,
  onSelectName,
}: {
  shift: RotaShift;
  selectedName: string;
  onSelectName: (name: string) => void;
}) {
  const isClosed = shift.teamLead === "CLOSED";

  function handleNameClick(name: string) {
    onSelectName(name === selectedName ? "" : name);
  }

  return (
    <div className={`shift-card${isClosed ? " closed" : ""}`}>
      <div className="shift-date">{formatBritishDate(shift.date)}</div>
      <div className="shift-team-lead">
        {isClosed ? (
          <span className="shift-closed-label">Closed</span>
        ) : shift.teamLead ? (
          <span
            className={`team-lead-chip clickable${shift.teamLead === selectedName ? " match" : ""}`}
            onClick={() => handleNameClick(shift.teamLead)}
          >
            {shift.teamLead}
          </span>
        ) : null}
      </div>
      {!isClosed && (
        <div className="shift-volunteers">
          {shift.volunteers.map((vol, i) => (
            <span
              key={i}
              className={`volunteer-name clickable${vol === selectedName ? " match" : ""}`}
              onClick={() => handleNameClick(vol)}
            >
              {vol}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

export default function RotaViewer({ rotaShifts }: RotaViewerProps) {
  const [selectedName, setSelectedName] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const allNames = useMemo(() => getAllNames(rotaShifts), [rotaShifts]);
  const upcomingShifts = useMemo(
    () => getUpcomingShifts(rotaShifts, selectedName),
    [rotaShifts, selectedName],
  );

  const filteredNames = allNames.filter((n) =>
    n.toLowerCase().includes(inputValue.toLowerCase()),
  );

  const close = useCallback(() => {
    setOpen(false);
    setInputValue("");
    setActiveIndex(-1);
  }, []);

  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      if (!containerRef.current?.contains(e.target as Node)) close();
    }
    document.addEventListener("mousedown", onMouseDown);
    return () => document.removeEventListener("mousedown", onMouseDown);
  }, [close]);

  function handleFocus() {
    setOpen(true);
    setInputValue("");
    setActiveIndex(-1);
  }

  function handleBlur(e: React.FocusEvent) {
    if (!containerRef.current?.contains(e.relatedTarget as Node)) close();
  }

  function handleSelect(name: string) {
    setSelectedName(name);
    close();
  }

  function handleClear(e: React.MouseEvent) {
    e.stopPropagation();
    setSelectedName("");
    setInputValue("");
    setOpen(false);
    inputRef.current?.focus();
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!open) {
      if (e.key === "ArrowDown" || e.key === "Enter") setOpen(true);
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, filteredNames.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, -1));
    } else if (e.key === "Enter" && activeIndex >= 0) {
      handleSelect(filteredNames[activeIndex]);
    } else if (e.key === "Escape") {
      close();
    }
  }

  return (
    <div className="rota-viewer">
      <h1>Ilford Drop-in Rota</h1>

      <div className="rota-search-wrap" ref={containerRef}>
        <div className={`rota-search-input-wrap${open ? " open" : ""}`}>
          <input
            ref={inputRef}
            type="text"
            className="rota-search"
            placeholder="Search by name…"
            value={open ? inputValue : selectedName}
            onChange={(e) => { setInputValue(e.target.value); setActiveIndex(-1); }}
            onFocus={handleFocus}
            onBlur={handleBlur}
            onKeyDown={handleKeyDown}
          />
          {selectedName && !open && (
            <button
              className="rota-search-clear"
              onMouseDown={handleClear}
              tabIndex={-1}
              aria-label="Clear"
            >
              <svg viewBox="0 0 14 14" width="14" height="14">
                <path
                  d="M13 1L1 13M1 1l12 12"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                />
              </svg>
            </button>
          )}
        </div>

        {open && (
          <ul className="rota-search-dropdown" role="listbox">
            {filteredNames.length === 0 ? (
              <li className="rota-search-no-options">No names found</li>
            ) : (
              filteredNames.map((name, i) => (
                <li
                  key={name}
                  role="option"
                  aria-selected={name === selectedName}
                  className={`rota-search-option${i === activeIndex ? " active" : ""}${name === selectedName ? " selected" : ""}`}
                  onMouseDown={() => handleSelect(name)}
                  onMouseEnter={() => setActiveIndex(i)}
                >
                  {name}
                </li>
              ))
            )}
          </ul>
        )}
      </div>

      {selectedName && (
        <div className="upcoming-strip">
          {upcomingShifts.length > 0 ? (
            <>
              <span className="upcoming-strip-label">Upcoming: </span>
              <span className="upcoming-dates">
                {upcomingShifts
                  .slice(0, 5)
                  .map((s) => formatBritishDate(s.date))
                  .join(" · ")}
              </span>
            </>
          ) : (
            <span className="upcoming-none">No upcoming shifts for {selectedName}</span>
          )}
        </div>
      )}

      <div className="rota-table">
        <div className="rota-table-header">
          <div className="header-row">
            <th>Date</th>
            <th>Team Lead</th>
            <th>Volunteers</th>
          </div>
        </div>
        <div className="rota-table-body">
          {rotaShifts.map((shift, i) => (
            <ShiftCard
              key={i}
              shift={shift}
              selectedName={selectedName}
              onSelectName={setSelectedName}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
