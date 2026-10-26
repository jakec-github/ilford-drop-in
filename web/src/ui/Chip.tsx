import "./Chip.css";
import { useChipSelection } from "./useChipSelection";

// Chip is a name as a small outlined token. It is read-only unless it is
// given onRemove, which draws a × inside it. Given a selectionKey, pressing its
// name selects it, along with every other chip on the page with that key.
//
// The rota draws its own chips as buttons with drag and menus attached, and
// takes only the classes from here: see Chip.css.
export default function Chip({
  label,
  shape = "pill",
  selected = false,
  selectionKey,
  removeLabel,
  onRemove,
  className,
}: {
  label: string;
  shape?: "pill" | "tag";
  selected?: boolean;
  // What makes this chip the same as another: see ChipSelection.
  selectionKey?: string;
  // What a screen reader hears for the × button. Defaults to "Remove <label>".
  removeLabel?: string;
  onRemove?: () => void;
  className?: string;
}) {
  const selection = useChipSelection(selectionKey);
  const selectable = selectionKey !== undefined;
  const classes = [
    "chip",
    `chip--${shape}`,
    selectable ? "" : "chip--static",
    "chip--enter",
    selected || selection.selected ? "selected" : "",
    className ?? "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <span className={classes}>
      {selectable ? (
        <button
          type="button"
          className="chip-select"
          aria-pressed={selection.selected}
          onClick={selection.toggle}
        >
          {label}
        </button>
      ) : (
        label
      )}
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
