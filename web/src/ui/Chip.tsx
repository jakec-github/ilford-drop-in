import "./Chip.css";

// Chip is a name as a small outlined token. It is read-only unless it is
// given onRemove, which draws a × inside it.
//
// The rota draws its own chips as buttons with drag and menus attached, and
// takes only the classes from here: see Chip.css.
export default function Chip({
  label,
  shape = "pill",
  selected = false,
  removeLabel,
  onRemove,
  className,
}: {
  label: string;
  shape?: "pill" | "tag";
  selected?: boolean;
  // What a screen reader hears for the × button. Defaults to "Remove <label>".
  removeLabel?: string;
  onRemove?: () => void;
  className?: string;
}) {
  const classes = [
    "chip",
    `chip--${shape}`,
    "chip--static",
    "chip--enter",
    selected ? "selected" : "",
    className ?? "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <span className={classes}>
      {label}
      {onRemove && (
        <button
          type="button"
          className="chip-remove"
          aria-label={removeLabel ?? `Remove ${label}`}
          onClick={onRemove}
        >
          ×
        </button>
      )}
    </span>
  );
}
