import { useCallback, useState } from "react";
import { defineRota } from "../api";
import type { DefinedRota } from "../types";

interface UseDefineRota {
  // The rota defined by the last successful call, or null before there is one.
  // Not a cache of server state: defining is not idempotent, so this is a record
  // of what this admin just created.
  rota: DefinedRota | null;
  error: string | null;
  defining: boolean;
  define: (shiftCount: number) => Promise<void>;
}

// useDefineRota owns the one admin action of defining a rota, and the outcome of
// the last attempt. A rejection (a bad shift count) resolves into `error` rather
// than rejecting: the view shows it inline, and a failed attempt leaves the
// previous result standing rather than blanking the panel.
export function useDefineRota(): UseDefineRota {
  const [rota, setRota] = useState<DefinedRota | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [defining, setDefining] = useState(false);

  const define = useCallback(async (shiftCount: number) => {
    setDefining(true);
    try {
      setRota(await defineRota(shiftCount));
      setError(null);
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to define the rota",
      );
    } finally {
      setDefining(false);
    }
  }, []);

  return { rota, error, defining, define };
}
