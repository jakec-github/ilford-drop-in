import type { SaveState } from "../hooks/useAutosave";
import "./SaveStatus.css";

// SaveStatus is the quiet word by a card's title that says it saved itself.
// Always in the layout, empty when there is nothing to say, so the title
// beside it never moves.
export default function SaveStatus({ state }: { state: SaveState }) {
  return (
    <span className={`save-status save-status--${state}`} aria-live="polite">
      {state === "saving" ? "Saving…" : state === "saved" ? "Saved" : ""}
    </span>
  );
}
