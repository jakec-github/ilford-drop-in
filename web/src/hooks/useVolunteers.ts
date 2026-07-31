import { useCallback, useEffect, useState } from "react";
import { fetchVolunteers, syncVolunteers } from "../api";
import type { Volunteer } from "../types";

export type SyncState = "idle" | "syncing" | "ok" | "error";

interface UseVolunteers {
  // null while the first load is still in flight; [] is a genuinely empty
  // roster, which the caller renders differently from "not loaded yet".
  volunteers: Volunteer[] | null;
  error: string | null;
  syncState: SyncState;
  sync: () => Promise<void>;
}

interface UseVolunteersOptions {
  // The roster is admin-only, so a view that shows it conditionally must be
  // able to say "not yet": fetching it for a logged-out visitor would be a
  // guaranteed 401 rendered as a load failure. Defaults to true.
  enabled?: boolean;
}

// useVolunteers owns the admin roster: the read and the sync that invalidates
// it. They belong together because a sync is only worth firing to change what
// the list shows, so the hook reloads on success and the view never has to
// remember to.
export function useVolunteers({
  enabled = true,
}: UseVolunteersOptions = {}): UseVolunteers {
  const [volunteers, setVolunteers] = useState<Volunteer[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [syncState, setSyncState] = useState<SyncState>("idle");

  // Written with .then rather than await so no setState is reached synchronously
  // from the effect below. A failed load reports itself through `error` and does
  // not reject: a caller reloading after a sync is reporting on the sync, not on
  // the reload.
  const load = useCallback(
    () =>
      fetchVolunteers()
        .then((loaded) => {
          setVolunteers(loaded);
          setError(null);
        })
        .catch((err: unknown) => {
          setError(
            err instanceof Error ? err.message : "Failed to load volunteers",
          );
        }),
    [],
  );

  useEffect(() => {
    if (enabled) void load();
  }, [enabled, load]);

  const sync = useCallback(async () => {
    setSyncState("syncing");
    try {
      await syncVolunteers();
      // Reload before reporting success, so "Synced" is never shown next to the
      // pre-sync list.
      await load();
      setSyncState("ok");
    } catch {
      setSyncState("error");
    }
  }, [load]);

  return { volunteers, error, syncState, sync };
}
