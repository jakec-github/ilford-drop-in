import { useCallback, useEffect, useState } from "react";
import {
  addOverride,
  fetchOverrides,
  removeOverride,
  reorderOverrides,
  updateOverride,
} from "../api";
import type { Override, OverrideDraft } from "../types";

interface UseOverrides {
  // null while the first load is still in flight; [] is "nothing overridden",
  // which the card says differently from "not loaded yet".
  overrides: Override[] | null;
  error: string | null;
  // Each write rejects with the server's own message — which line of the rule
  // it could not read, who is pinned twice — and the caller shows it.
  add: (draft: OverrideDraft) => Promise<void>;
  update: (id: string, draft: OverrideDraft) => Promise<void>;
  remove: (id: string) => Promise<void>;
  // Moves one a place earlier (-1) or later (+1) in the list.
  move: (id: string, by: -1 | 1) => Promise<void>;
}

// useOverrides owns the Rota Defaults' Overrides. The settings screen and the
// define screen both read it, as they both read the rest of the Rota Defaults.
export function useOverrides(): UseOverrides {
  const [overrides, setOverrides] = useState<Override[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reloads, setReloads] = useState(0);

  useEffect(() => {
    let cancelled = false;
    fetchOverrides()
      .then((loaded) => {
        if (cancelled) return;
        setOverrides(loaded);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : "Failed to load the overrides",
        );
      });
    return () => {
      cancelled = true;
    };
  }, [reloads]);

  // Every write answers with the whole list, which is held rather than read
  // again. A refused one reloads, because a refusal is most often a list out
  // of date — somebody else moved or removed one — and re-throws to say why.
  const hold = useCallback(async (write: Promise<Override[]>) => {
    try {
      setOverrides(await write);
      setError(null);
    } catch (err) {
      setReloads((n) => n + 1);
      throw err;
    }
  }, []);

  const add = useCallback(
    (draft: OverrideDraft) => hold(addOverride(draft)),
    [hold],
  );
  const update = useCallback(
    (id: string, draft: OverrideDraft) => hold(updateOverride(id, draft)),
    [hold],
  );
  const remove = useCallback((id: string) => hold(removeOverride(id)), [hold]);
  const move = useCallback(
    async (id: string, by: -1 | 1) => {
      if (!overrides) return;
      const ids = overrides.map((o) => o.id);
      const from = ids.indexOf(id);
      const to = from + by;
      if (from < 0 || to < 0 || to >= ids.length) return;
      [ids[from], ids[to]] = [ids[to], ids[from]];
      await hold(reorderOverrides(ids));
    },
    [hold, overrides],
  );

  return { overrides, error, add, update, remove, move };
}
