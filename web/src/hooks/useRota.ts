import { useCallback, useEffect, useState } from "react";
import {
  createAlteration,
  fetchRota,
  setShiftClosed,
  setShiftTimes,
} from "../api";
import type { RotaChange, RotaShift } from "../types";

interface UseRota {
  // null while the first load is still in flight; [] is a rota with no shifts,
  // which the caller renders differently from "not loaded yet".
  shifts: RotaShift[] | null;
  error: string | null;
  // change records one alteration and reloads the rota, whether or not the
  // change was accepted. It rejects with the server's message when it was not.
  change: (change: RotaChange) => Promise<void>;
  // setClosed shuts or reopens one shift, reloading on the same terms. Not an
  // alteration: it changes what allocation will do rather than what an
  // allocated rota says.
  setClosed: (shiftId: string, closed: boolean) => Promise<void>;
  // setTimes moves one shift's hours, and with them its date. Also not an
  // alteration, and not frozen at allocation either: the times describe the
  // shift rather than feed the solver.
  setTimes: (shiftId: string, start: string, end: string) => Promise<void>;
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

  const setClosed = useCallback(
    async (shiftId: string, closed: boolean) => {
      try {
        await setShiftClosed(shiftId, closed);
      } finally {
        await load();
      }
    },
    [load],
  );

  const setTimes = useCallback(
    async (shiftId: string, start: string, end: string) => {
      try {
        await setShiftTimes(shiftId, start, end);
      } finally {
        await load();
      }
    },
    [load],
  );

  return { shifts, error, change, setClosed, setTimes };
}
