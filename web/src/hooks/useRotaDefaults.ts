import { useCallback, useEffect, useState } from "react";
import { fetchRotaDefaults, saveShiftTimeDefaults } from "../api";
import type { RotaDefaults } from "../types";

interface UseRotaDefaults {
  // null while the first load is still in flight. A loaded record with empty
  // times is a different thing entirely: it means an admin has not set them.
  defaults: RotaDefaults | null;
  error: string | null;
  // Writes the shift times and holds what the server stored — including the
  // timezone it filled in for a blank field. Rejects with the server's own
  // message, which names the field that was wrong.
  saveShiftTimes: (defaults: RotaDefaults) => Promise<void>;
}

// useRotaDefaults owns the settings an admin keeps for the drop-in as a whole.
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

  // Holds the answer rather than re-reading: the write returns the whole record
  // as it now stands, so a second round trip would only confirm what is already
  // in hand. A refusal changes nothing on the server, so what is held stays
  // right, and the caller re-throws to say why.
  const saveShiftTimes = useCallback(async (next: RotaDefaults) => {
    const saved = await saveShiftTimeDefaults(next);
    setDefaults(saved);
    setError(null);
  }, []);

  return { defaults, error, saveShiftTimes };
}
