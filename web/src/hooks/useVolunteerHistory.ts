import { useEffect, useState } from "react";
import { fetchVolunteerHistory } from "../api";
import type { VolunteerHistory } from "../types";

interface UseVolunteerHistory {
  // null while loading.
  history: VolunteerHistory | null;
  error: string | null;
}

export function useVolunteerHistory(): UseVolunteerHistory {
  const [history, setHistory] = useState<VolunteerHistory | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    fetchVolunteerHistory()
      .then((loaded) => {
        if (live) setHistory(loaded);
      })
      .catch((err: unknown) => {
        if (live)
          setError(
            err instanceof Error ? err.message : "Failed to load history",
          );
      });
    return () => {
      live = false;
    };
  }, []);

  return { history, error };
}
