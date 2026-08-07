import { useCallback, useEffect, useState } from "react";
import { defineRota, fetchRotaProposal } from "../api";
import type { DefinedRota, NewRota, RotaProposal } from "../types";

interface UseDefineRota {
  // What the form starts from, or null while a read is in flight. Null is what
  // the view renders as "loading", and what makes a reload remount the form on
  // the new answer rather than leaving it on the old one.
  proposal: RotaProposal | null;
  // The rota defined by the last successful call, or null before there is one.
  // Not a cache of server state: defining is not idempotent, so this is a record
  // of what this admin just created.
  rota: DefinedRota | null;
  error: string | null;
  defining: boolean;
  define: (rota: NewRota) => Promise<void>;
}

// useDefineRota owns the one admin action of defining a rota: what the form
// starts from, the attempt, and its outcome.
//
// The proposal is here rather than in a hook of its own because it is not a
// resource anything else reads — it exists to fill this form in, and its fields
// are the fields `define` takes back. A rejection (a bad shift count, a date
// that already has a shift) resolves into `error` rather than rejecting: the
// view shows it inline, and a failed attempt leaves the form standing on what
// was typed.
export function useDefineRota(): UseDefineRota {
  const [proposal, setProposal] = useState<RotaProposal | null>(null);
  const [rota, setRota] = useState<DefinedRota | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [defining, setDefining] = useState(false);

  // Read once, on mount, and never refreshed. The proposal counts forward from
  // the last rota, so it goes stale the moment one is defined or discarded —
  // but the screen that shows it is the Allocation tab with nothing in flight,
  // which is unmounted on the first of those and remounted on the second. A
  // remount is the reload.
  useEffect(() => {
    let cancelled = false;
    fetchRotaProposal()
      .then((loaded) => {
        if (cancelled) return;
        setProposal(loaded);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : "Failed to load the next rota",
        );
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const define = useCallback(async (next: NewRota) => {
    setDefining(true);
    try {
      setRota(await defineRota(next));
      setError(null);
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to define the rota",
      );
    } finally {
      setDefining(false);
    }
  }, []);

  return { proposal, rota, error, defining, define };
}
