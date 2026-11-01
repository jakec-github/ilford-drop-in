import "./ModeSwitch.css";

// How something is written: described in plain English and translated (ADR
// 0015), or raw, as the iCalendar lines themselves.
export type EditorMode = "natural" | "raw";

const OPTIONS: [EditorMode, string][] = [
  ["natural", "Natural"],
  ["raw", "Raw"],
];

// ModeSwitch says which of the two editors is showing. When it cannot be
// moved, the reason is on hover — on the group, because a disabled button
// shows no title in every browser.
export default function ModeSwitch({
  label,
  mode,
  onMode,
  locked,
}: {
  // What it switches, as a screen reader names the group.
  label: string;
  mode: EditorMode;
  onMode: (mode: EditorMode) => void;
  // Why the switch cannot be moved, or null when it can.
  locked: string | null;
}) {
  return (
    <div
      className="mode-switch"
      role="group"
      aria-label={label}
      title={locked ?? undefined}
    >
      {OPTIONS.map(([value, name]) => (
        <button
          key={value}
          type="button"
          className="mode-switch-option"
          aria-pressed={mode === value}
          disabled={locked !== null}
          onClick={() => onMode(value)}
        >
          {name}
        </button>
      ))}
    </div>
  );
}
