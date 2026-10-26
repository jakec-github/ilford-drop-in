import { useCallback, useEffect, useState } from "react";
import {
  addAllocationRule,
  editAllocationRule,
  fetchRotaDefaults,
  removeAllocationRule,
  saveDefaultShape,
  saveShiftTimeDefaults,
} from "../api";
import type {
  AllocationRuleDraft,
  AllocationRuleEdit,
  RotaDefaults,
  ShiftTimes,
} from "../types";

interface UseRotaDefaults {
  // null while the first load is still in flight. A loaded record with empty
  // times is a different thing entirely: it means an Organiser has not set them.
  defaults: RotaDefaults | null;
  error: string | null;
  // Writes the shift times and holds what the server stored — including the
  // timezone it filled in for a blank field. Rejects with the server's own
  // message, which names the field that was wrong.
  saveShiftTimes: (times: ShiftTimes) => Promise<void>;
  // Writes the Shape whole: the Seats given here are the Seats a shift asks
  // for, and a Role left out is one it no longer asks for.
  saveShape: (seats: { roleId: string; count: number }[]) => Promise<void>;
  // Add, edit and remove one Allocation Rule each, rejecting with the server's
  // own message when it refuses.
  addRule: (draft: AllocationRuleDraft) => Promise<void>;
  editRule: (id: string, edit: AllocationRuleEdit) => Promise<void>;
  removeRule: (id: string) => Promise<void>;
}

// useRotaDefaults owns the settings an Organiser keeps for the Organisation as a whole.
// Only the settings screen reads it today; the rota and the availability pages
// get their times already resolved, on the shifts they render.
export function useRotaDefaults(): UseRotaDefaults {
  const [defaults, setDefaults] = useState<RotaDefaults | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchRotaDefaults()
      .then((loaded) => {
        if (cancelled) return;
        setDefaults(loaded);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : "Failed to load the settings",
        );
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Every write holds its answer rather than re-reading: each returns the whole
  // record as it now stands, so a second round trip would only confirm what is
  // already in hand. A refusal changes nothing on the server, so what is held
  // stays right, and the caller re-throws to say why.
  const hold = useCallback(async (write: Promise<RotaDefaults>) => {
    const saved = await write;
    setDefaults(saved);
    setError(null);
  }, []);

  const saveShiftTimes = useCallback(
    (next: ShiftTimes) => hold(saveShiftTimeDefaults(next)),
    [hold],
  );
  const saveShape = useCallback(
    (seats: { roleId: string; count: number }[]) =>
      hold(saveDefaultShape(seats)),
    [hold],
  );
  const addRule = useCallback(
    (draft: AllocationRuleDraft) => hold(addAllocationRule(draft)),
    [hold],
  );
  const editRule = useCallback(
    (id: string, edit: AllocationRuleEdit) =>
      hold(editAllocationRule(id, edit)),
    [hold],
  );
  const removeRule = useCallback(
    (id: string) => hold(removeAllocationRule(id)),
    [hold],
  );

  return {
    defaults,
    error,
    saveShiftTimes,
    saveShape,
    addRule,
    editRule,
    removeRule,
  };
}
