import { useState, type ReactNode } from "react";
import { ChipSelectionContext, type Selection } from "./useChipSelection";

// The page-wide holder of the chip selection: see useChipSelection.ts.
export function ChipSelectionProvider({
  value,
  onChange,
  children,
}: {
  value?: string | null;
  onChange?: (key: string | null) => void;
  children: ReactNode;
}) {
  const [own, setOwn] = useState<string | null>(null);
  const controlled = value !== undefined;
  const selection: Selection = {
    selected: controlled ? value : own,
    select: (key) => {
      if (!controlled) setOwn(key);
      onChange?.(key);
    },
  };
  return (
    <ChipSelectionContext.Provider value={selection}>
      {children}
    </ChipSelectionContext.Provider>
  );
}

