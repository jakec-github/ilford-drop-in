import { useCallback, useEffect, useState } from "react";
import { discardRota, fetchRotaInFlight } from "../api";
import type { RotaInFlight } from "../types";

interface UseRotaInFlight {
  // The rota being worked on, or null when there is none — which is the state
  // in which one may be defined. `loading` is what tells the two apart from the
  // first read not having landed yet.
  inFlight: RotaInFlight | null;
  loading: boolean;
  error: string | null;
  reload: () => Promise<void>;
  // discard destroys the rota and everything hanging off it, then re-reads.
  // It rejects with the server's message when the rota is refused — an
  // allocated one is never discarded — so the caller can show it.
  discard: (id: string) => Promise<void>;
}

// useRotaInFlight owns the one-rota-in-flight state: what is being worked on,
// and the discard that ends it.
//
// They belong together because discarding is only ever done to the rota this
// read named, and the answer to "did it work" is the next read of the same
// thing — the point of a discard is that nothing is in flight afterwards.
//
// Defining is not here. It is its own action with its own result to show (see
// useDefineRota), and its effect on this hook is the ordinary one of any write:
// the caller reloads.
export function useRotaInFlight(): UseRotaInFlight {
  const [inFlight, setInFlight] = useState<RotaInFlight | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Written with .then rather than await so no setState is reached
  // synchronously from the effect below. A failed load reports itself through
  // `error` and does not reject.
  const load = useCallback(
    () =>
      fetchRotaInFlight()
        .then((loaded) => {
          setInFlight(loaded);
          setError(null);
        })
        .catch((err: unknown) => {
          setError(err instanceof Error ? err.message : "Failed to load");
        })
        .finally(() => {
          setLoading(false);
        }),
    [],
  );

  useEffect(() => {
    void load();
  }, [load]);

  // Reloading in a finally, not after a success: a refused discard is the
  // moment the shown rota is most likely to be wrong — the usual reason is that
  // it was allocated while this page was open, and the re-read is what makes
  // the message make sense.
  const discard = useCallback(
    async (id: string) => {
      try {
        await discardRota(id);
      } finally {
        await load();
      }
    },
    [load],
  );

  return { inFlight, loading, error, reload: load, discard };
}
