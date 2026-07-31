import { useCallback, useEffect, useState } from "react";
import { createAlteration, fetchRota } from "../api";
import type { RotaChange, RotaShift } from "../types";

interface UseRota {
  // null while the first load is still in flight; [] is a rota with no shifts,
  // which the caller renders differently from "not loaded yet".
  shifts: RotaShift[] | null;
  error: string | null;
  // change records one alteration and reloads the rota, whether or not the
  // change was accepted. It rejects with the server's message when it was not.
  change: (change: RotaChange) => Promise<void>;
}

// useRota owns the rota the page shows: the read and the changes that
// invalidate it. They belong together because the server layers alterations
// over allocations — a change's real outcome (who ends up where, and in which
// role) only exists in the next GET /shifts, so the hook re-reads rather than
// hand the caller a patched copy to trust.
export function useRota(): UseRota {
  const [shifts, setShifts] = useState<RotaShift[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Written with .then rather than await so no setState is reached
  // synchronously from the effect below. A failed load reports itself through
  // `error` and does not reject: a caller reloading after a change is reporting
  // on the change, not on the reload.
  const load = useCallback(
    () =>
      fetchRota()
        .then((loaded) => {
          setShifts(loaded);
          setError(null);
        })
        .catch((err: unknown) => {
          setError(err instanceof Error ? err.message : "Failed to load rota");
        }),
    [],
  );

  useEffect(() => {
    void load();
  }, [load]);

  // Reloading in a finally, not after a success: a refusal is the moment the
  // shown rota is most likely to be wrong. The usual reason a change is
  // refused is that the shift is no longer what this page thinks it is, so the
  // re-read is what makes the message make sense.
  const change = useCallback(
    async (rotaChange: RotaChange) => {
      try {
        await createAlteration(rotaChange);
      } finally {
        await load();
      }
    },
    [load],
  );

  return { shifts, error, change };
}
