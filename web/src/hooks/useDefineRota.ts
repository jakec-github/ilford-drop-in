import { useCallback, useEffect, useState } from "react";
import { defineRota, fetchRotaProposal } from "../api";
import type { DefinedRota, NewRota, RotaProposal } from "../types";

interface UseDefineRota {
  // What the form starts from, or null while a read is in flight. Null is what
  // the view renders as "loading", and what makes a reload remount the form on
  // the new answer rather than leaving it on the old one.
  proposal: RotaProposal | null;
  // Re-reads the proposal. Wanted after a discard: the rota that was destroyed
  // is the rota this counted forward from, so the date it named is now a week
  // or more too late.
  reloadProposal: () => void;
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
  // Bumped to ask for the proposal again. The read lives in one effect either
  // way, so a reload cancels an in-flight one exactly as unmounting does.
  const [attempt, setAttempt] = useState(0);

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
  }, [attempt]);

  // Clearing the proposal is what makes this a reload rather than a second
  // answer arriving beside the first: the form is unmounted while the read is
  // on its way and remounts on the new one. A form left standing would keep
  // every field initialised from an answer that has been superseded.
  const reloadProposal = useCallback(() => {
    setProposal(null);
    setAttempt((n) => n + 1);
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

  return { proposal, reloadProposal, rota, error, defining, define };
}
