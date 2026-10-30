import { useCallback, useEffect, useSyncExternalStore } from "react";
import {
  addAllocationRule,
  editAllocationRule,
  fetchRotaDefaults,
  removeAllocationRule,
  saveDefaultShape,
  saveShiftPattern as saveShiftPatternSection,
  saveTimezone as saveTimezoneSection,
} from "../api";
import type {
  AllocationRuleDraft,
  AllocationRuleEdit,
  RotaDefaults,
} from "../types";

interface UseRotaDefaults {
  // null while the first load is still in flight. A loaded record with an empty
  // pattern is a different thing entirely: it means an Organiser has not set one.
  defaults: RotaDefaults | null;
  error: string | null;
  // Writes the Shift pattern and holds what the server stored. Rejects with
  // the server's own message, which names the entry that was wrong.
  saveShiftPattern: (pattern: string) => Promise<void>;
  // Writes the zone the Organisation's times are read in. Rejects with the
  // server's message when it cannot load the zone.
  saveTimezone: (zone: string) => Promise<void>;
  // Writes the Shape whole: the Seats given here are the Seats a shift asks
  // for, and a Role left out is one it no longer asks for.
  saveShape: (seats: { roleId: string; count: number }[]) => Promise<void>;
  // Add, edit and remove one Allocation Rule each, rejecting with the server's
  // own message when it refuses.
  addRule: (draft: AllocationRuleDraft) => Promise<void>;
  editRule: (id: string, edit: AllocationRuleEdit) => Promise<void>;
  removeRule: (id: string) => Promise<void>;
}

interface RotaDefaultsSnapshot {
  defaults: RotaDefaults | null;
  error: string | null;
}

// One copy of the record for every section that reads it, as useRoles keeps
// one list of Roles. Config hides its later sections until the Rota card is
// set, and with a copy per hook, saving the pattern left the screen around it
// still believing it was not.
let snapshot: RotaDefaultsSnapshot = { defaults: null, error: null };
const readers = new Set<() => void>();

function publish(next: RotaDefaultsSnapshot) {
  snapshot = next;
  for (const reader of readers) reader();
}

function subscribe(reader: () => void): () => void {
  readers.add(reader);
  return () => {
    readers.delete(reader);
  };
}

function currentSnapshot(): RotaDefaultsSnapshot {
  return snapshot;
}

// The read in flight, so several sections mounting at once ask once.
let reading: Promise<void> | null = null;

function load(): Promise<void> {
  if (reading) return reading;
  reading = fetchRotaDefaults()
    .then((loaded) => publish({ defaults: loaded, error: null }))
    .catch((err: unknown) => {
      // What is already held survives a failed re-read: it is the last thing
      // the server said.
      publish({
        defaults: snapshot.defaults,
        error:
          err instanceof Error ? err.message : "Failed to load the settings",
      });
    })
    .finally(() => {
      reading = null;
    });
  return reading;
}

// Every write holds its answer rather than re-reading: each returns the whole
// record as it now stands, so a second round trip would only confirm what is
// already in hand. A refusal changes nothing on the server, so what is held
// stays right, and the caller re-throws to say why.
async function hold(write: Promise<RotaDefaults>) {
  publish({ defaults: await write, error: null });
}

// useRotaDefaults owns the settings an Organiser keeps for the Organisation as a
// whole. Config and the define screen read it; the rota and the availability
// pages get their times already resolved, on the shifts they render.
//
// enabled is for a screen that reads it only for an Organiser: the record is
// Organiser-only, and asking for it signed out is a guaranteed refusal.
export function useRotaDefaults({
  enabled = true,
}: { enabled?: boolean } = {}): UseRotaDefaults {
  const { defaults, error } = useSyncExternalStore(subscribe, currentSnapshot);

  // Opening a screen is the moment to find out what has changed since.
  useEffect(() => {
    if (enabled) void load();
  }, [enabled]);

  const saveShiftPattern = useCallback(
    (next: string) => hold(saveShiftPatternSection(next)),
    [],
  );
  const saveTimezone = useCallback(
    (zone: string) => hold(saveTimezoneSection(zone)),
    [],
  );
  const saveShape = useCallback(
    (seats: { roleId: string; count: number }[]) =>
      hold(saveDefaultShape(seats)),
    [],
  );
  const addRule = useCallback(
    (draft: AllocationRuleDraft) => hold(addAllocationRule(draft)),
    [],
  );
  const editRule = useCallback(
    (id: string, edit: AllocationRuleEdit) =>
      hold(editAllocationRule(id, edit)),
    [],
  );
  const removeRule = useCallback(
    (id: string) => hold(removeAllocationRule(id)),
    [],
  );

  return {
    defaults: enabled ? defaults : null,
    error: enabled ? error : null,
    saveShiftPattern,
    saveTimezone,
    saveShape,
    addRule,
    editRule,
    removeRule,
  };
}

// patternSet and shapeSet are the two halves of the Rota card a rota cannot be
// defined without. One definition, so the card's own "not set yet" and the gate
// in front of the rest of the app cannot disagree.
export function patternSet(defaults: RotaDefaults): boolean {
  return defaults.shiftPattern !== "";
}

export function shapeSet(defaults: RotaDefaults): boolean {
  return defaults.defaultShape.length > 0;
}
