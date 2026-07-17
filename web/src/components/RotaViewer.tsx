import { useMemo, useState, useRef, useEffect, useCallback } from "react";
import type { Assignee, RotaShift } from "../types";
import "./RotaViewer.css";

interface RotaViewerProps {
  rotaShifts: RotaShift[];
}

// "2 Feb" — weekday and year are redundant on the rota.
function formatShiftDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

// Group membership is shown by a corner dot; the colour just needs to be stable
// per group and tell two groups apart, so we hash the group key into this list.
// The eight are mid-toned (legible on both themes) and spread so the closest
// pair still sits ~32 ΔE apart — no two land as look-alikes.
const GROUP_COLOURS = [
  "#d6455a",
  "#e2711d",
  "#c7a92b",
  "#4c9a2a",
  "#1ca3a3",
  "#3d6fd6",
  "#8b52d6",
  "#c94fa0",
];

function groupColour(key: string): string {
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return GROUP_COLOURS[hash % GROUP_COLOURS.length];
}

function getAllNames(shifts: RotaShift[]): string[] {
  const names = new Set<string>();
  for (const shift of shifts) {
    for (const a of shift.assignees) names.add(a.name);
  }
  return Array.from(names).sort();
}

function getUpcomingShifts(shifts: RotaShift[], name: string): RotaShift[] {
  if (!name) return [];
  return shifts.filter((shift) => shift.assignees.some((a) => a.name === name));
}

function Chip({
  assignee,
  selected,
  onClick,
}: {
  assignee: Assignee;
  selected: boolean;
  onClick: () => void;
}) {
  const cls = [
    "chip",
    `role-${assignee.role}`,
    assignee.custom ? "custom" : "volunteer",
    selected ? "selected" : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <span className={cls} onClick={onClick}>
      {assignee.name}
      {assignee.group && (
        <span
          className="chip-group-dot"
          style={{ background: groupColour(assignee.group) }}
          title={`Group: ${assignee.group}`}
        />
      )}
    </span>
  );
}

function ShiftRow({
  shift,
  selectedName,
  onSelectName,
}: {
  shift: RotaShift;
  selectedName: string;
  onSelectName: (name: string) => void;
}) {
  function handleClick(name: string) {
    onSelectName(name === selectedName ? "" : name);
  }

  return (
    <div className={`shift-row${shift.closed ? " closed" : ""}`}>
      <div className="shift-date">{formatShiftDate(shift.date)}</div>
      {shift.closed ? (
        <span className="shift-closed-label">Closed</span>
      ) : (
        <div className="shift-people">
          {shift.assignees.map((a, i) => (
            <Chip
              key={i}
              assignee={a}
              selected={a.name === selectedName}
              onClick={() => handleClick(a.name)}
            />
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
                  .map((s) => formatShiftDate(s.date))
                  .join(" · ")}
              </span>
            </>
          ) : (
            <span className="upcoming-none">No upcoming shifts for {selectedName}</span>
          )}
        </div>
      )}

      <div className="rota-list">
        {rotaShifts.map((shift, i) => (
          <ShiftRow
            key={i}
            shift={shift}
            selectedName={selectedName}
            onSelectName={setSelectedName}
          />
        ))}
      </div>
    </div>
  );
}
