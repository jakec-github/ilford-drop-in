import { useEffect, useState } from "react";
import { fetchPreallocations } from "../api";
import type { Preallocation } from "../types";

interface UsePreallocations {
  // null while the first load is still in flight; [] is "nobody is pinned",
  // which a caller renders differently from "not loaded yet".
  preallocations: Preallocation[] | null;
  error: string | null;
}

interface UsePreallocationsOptions {
  // Pins are admin-only, so a view that shows them conditionally must be able
  // to say "not yet": fetching them for a logged-out visitor would be a
  // guaranteed 401 rendered as a load failure. Defaults to true.
  enabled?: boolean;
}

// usePreallocations owns the pins the rota page shows against shifts that have
// not been allocated yet. Read-only: pins are created and removed over the API
// but nothing in the UI does that yet, so there is no invalidation to own.
export function usePreallocations({
  enabled = true,
}: UsePreallocationsOptions = {}): UsePreallocations {
  const [preallocations, setPreallocations] = useState<Preallocation[] | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);

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
  }, [enabled]);

  return { preallocations, error };
}
