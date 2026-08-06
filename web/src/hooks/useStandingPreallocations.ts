import { useCallback, useEffect, useState } from "react";
import {
  createStandingPreallocation,
  deleteStandingPreallocation,
  fetchStandingPreallocations,
} from "../api";
import type { NewStandingPreallocation, StandingPreallocation } from "../types";

interface UseStandingPreallocations {
  // null while the first load is still in flight; [] is "nothing is promised
  // every rota", which the settings screen renders differently from "not loaded
  // yet".
  standing: StandingPreallocation[] | null;
  error: string | null;
  // Adds one, then reloads. Rejects with the server's own message when the
  // write is refused — "Alice is already pinned on those shifts" is the whole
  // explanation, and the caller shows it rather than inventing one.
  addStanding: (standing: NewStandingPreallocation) => Promise<void>;
  // Removes one, then reloads. The pins it has already seeded belong to the
  // rotas that minted them and are left exactly as they are.
  removeStanding: (id: string) => Promise<void>;
}

// useStandingPreallocations owns the pins an admin has said to make every rota.
// Only the settings screen uses it: nothing else reads them, because defining a
// rota is the one moment they are spent.
export function useStandingPreallocations(): UseStandingPreallocations {
  const [standing, setStanding] = useState<StandingPreallocation[] | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [reloads, setReloads] = useState(0);

  useEffect(() => {
    let cancelled = false;
    void fetchStandingPreallocations()
      .then((loaded) => {
        if (cancelled) return;
        setStanding(loaded);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error
            ? err.message
            : "Failed to load the standing preallocations",
        );
      });
    return () => {
      cancelled = true;
    };
  }, [reloads]);

  // Reloads whether or not the write landed, then re-throws so the caller can
  // say why — the same discipline as useRoles, and for the same reason: a
  // refusal usually means what is held here is out of date.
  const write = useCallback(async (apply: () => Promise<void>) => {
    try {
      await apply();
    } finally {
      setReloads((n) => n + 1);
    }
  }, []);

  const addStanding = useCallback(
    (s: NewStandingPreallocation) => write(() => createStandingPreallocation(s)),
    [write],
  );

  const removeStanding = useCallback(
    (id: string) => write(() => deleteStandingPreallocation(id)),
    [write],
  );

  return { standing, error, addStanding, removeStanding };
}
