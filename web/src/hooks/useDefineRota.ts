import { useCallback, useEffect, useState } from "react";
import { defineRota, fetchRotaPlan } from "../api";
import type { DefinedRota, NewRota, RotaPlan } from "../types";

// How long typing has to pause before the plan is read again.
const SETTLE_MS = 250;

interface UseDefineRota {
  // The rota defining one now would make, for the count and gap last asked
  // about, or null before the first answer. Kept while a newer one is on its
  // way, so the preview does not blink out between keystrokes.
  plan: RotaPlan | null;
  // A request the server could not plan — a count out of range, say. A plan it
  // would refuse to define is not this: that is plan.refusal.
  planError: string | null;
  // The rota defined by the last successful call, or null before there is one.
  // Not a cache of server state: defining is not idempotent, so this is a record
  // of what this Organiser just created.
  rota: DefinedRota | null;
  error: string | null;
  defining: boolean;
  define: (rota: NewRota) => Promise<void>;
}

// useDefineRota owns the one Organiser action of defining a rota: the plan it
// would make, the attempt, and its outcome.
//
// The plan is the server's (ADR 0013): the same arithmetic as the definition,
// so what the screen previews is what defining mints. It is read again
// whenever the count, the gap or `reload` changes — the last for a change to
// the Rota Defaults beside it. With no count typed yet it is read for one
// shift, which is enough to say where the previous rota stopped and whether
// there is a pattern at all; the view shows no Shifts for it.
//
// A rejected define resolves into `error` rather than rejecting: the view shows
// it inline, and a failed attempt leaves the form standing on what was typed.
export function useDefineRota(
  shiftCount: number | null,
  notBefore: string,
  reload: number,
): UseDefineRota {
  const [plan, setPlan] = useState<RotaPlan | null>(null);
  const [planError, setPlanError] = useState<string | null>(null);
  const [rota, setRota] = useState<DefinedRota | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [defining, setDefining] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      fetchRotaPlan(
        { shiftCount: shiftCount ?? 1, notBefore },
        controller.signal,
      )
        .then((loaded) => {
          setPlan(loaded);
          setPlanError(null);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted) return;
          setPlanError(
            err instanceof Error ? err.message : "Failed to plan the next rota",
          );
        });
    }, SETTLE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [shiftCount, notBefore, reload]);

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

  return { plan, planError, rota, error, defining, define };
}
