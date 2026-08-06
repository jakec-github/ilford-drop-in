import { useCallback, useEffect, useState } from "react";
import {
  createPreallocation,
  deletePreallocation,
  fetchPreallocations,
} from "../api";
import type { NewPreallocation, Preallocation } from "../types";

interface UsePreallocations {
  // null while the first load is still in flight; [] is "nobody is pinned",
  // which a caller renders differently from "not loaded yet".
  preallocations: Preallocation[] | null;
  error: string | null;
  // Pins one person to a shift, then reloads. Rejects with the server's own
  // message when the pin is refused, which is the whole explanation of why —
  // the caller shows it rather than inventing one.
  addPin: (pin: NewPreallocation) => Promise<void>;
  // Removes one pin by id, then reloads. Any pin can go: there is one kind of
  // them, and an admin may take back any promise a rota has not been allocated
  // on.
  removePin: (id: string) => Promise<void>;
}

interface UsePreallocationsOptions {
  // Pins are admin-only, so a view that shows them conditionally must be able
  // to say "not yet": fetching them for a logged-out visitor would be a
  // guaranteed 401 rendered as a load failure. Defaults to true.
  enabled?: boolean;
}

// usePreallocations owns the pins the rota page shows against shifts that have
// not been allocated yet, and the two ways an admin changes them.
//
// A write re-reads the listing rather than patching what is held: pins are
// ordered server-side, and the server can refuse one. What comes back from the
// reload is what the allocator will actually be handed.
export function usePreallocations({
  enabled = true,
}: UsePreallocationsOptions = {}): UsePreallocations {
  const [preallocations, setPreallocations] = useState<Preallocation[] | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [reloads, setReloads] = useState(0);

  // Written with .then rather than await so no setState is reached
  // synchronously from the effect.
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    void fetchPreallocations()
      .then((loaded) => {
        if (cancelled) return;
        setPreallocations(loaded);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : "Failed to load preallocations",
        );
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, reloads]);

  const reload = useCallback(() => setReloads((n) => n + 1), []);

  // Reloads whether or not the write landed, then re-throws so the caller can
  // say why. A refusal is the case that most needs the re-read: the server
  // turned the write down because what is held here is no longer true — the pin
  // was already removed, someone else took the lead slot — so leaving the old
  // listing up would contradict the message next to it.
  const write = useCallback(
    async (apply: () => Promise<void>) => {
      try {
        await apply();
      } finally {
        reload();
      }
    },
    [reload],
  );

  const addPin = useCallback(
    (pin: NewPreallocation) => write(() => createPreallocation(pin)),
    [write],
  );

  const removePin = useCallback(
    (id: string) => write(() => deletePreallocation(id)),
    [write],
  );

  return { preallocations, error, addPin, removePin };
}
