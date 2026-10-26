import { createContext, useContext } from "react";

// ChipSelection is which chip the reader has picked out of a page: every chip
// drawn with the same key is highlighted with it. A key is whatever the caller
// says makes two chips the same thing — an id, never a label, since two chips
// may read alike and be different things.
//
// One provider per page, so a selection ends when the page does. A page that
// does something more with the selection than highlight it holds it itself and
// passes value and onChange; anywhere else the provider holds its own.
export interface Selection {
  selected: string | null;
  select: (key: string | null) => void;
}

export const ChipSelectionContext = createContext<Selection | null>(null);

// Whether the chip with this key is the selected one, and how to toggle it:
// picking the selected chip again clears the selection, picking another moves
// it. Outside a provider nothing is ever selected and toggling does nothing.
export function useChipSelection(key: string | null | undefined): {
  selected: boolean;
  toggle: () => void;
} {
  const selection = useContext(ChipSelectionContext);
  if (!selection || key == null) return { selected: false, toggle: () => {} };
  const selected = selection.selected === key;
  return {
    selected,
    toggle: () => selection.select(selected ? null : key),
  };
}
