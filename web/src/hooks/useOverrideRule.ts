import { useEffect, useState } from "react";
import { readOverrideRule } from "../api";
import type { OverrideRuleReading } from "../types";

// How long typing has to pause before the rule is read again, as for the
// Shift pattern.
const SETTLE_MS = 300;

// useOverrideRule reads an Override's rule while it is being written: what it
// says, whether it matches a time or whole dates, or what is wrong with it.
// The server does the reading, by the evaluator that mints a rota, so the
// description is what defining will do (ADR 0014). The rule carries the date
// it counts from as its own DTSTART line.
//
// The last reading is kept while a newer one is on its way, so the
// description does not blink out between keystrokes.
export function useOverrideRule(rrule: string): {
  reading: OverrideRuleReading | null;
  // Whether reading is of this rule, rather than of what was
  // typed before — a card that saves itself waits until it is.
  current: boolean;
  error: string | null;
} {
  const [read, setRead] = useState<{
    rrule: string;
    reading: OverrideRuleReading;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const blank = rrule.trim() === "";

  useEffect(() => {
    if (blank) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      readOverrideRule(rrule, controller.signal)
        .then((loaded) => {
          setRead({ rrule, reading: loaded });
          setError(null);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted) return;
          setError(
            err instanceof Error ? err.message : "Failed to read the rule",
          );
        });
    }, SETTLE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [blank, rrule]);

  if (blank) return { reading: null, current: false, error: null };
  return {
    reading: read?.reading ?? null,
    current: read?.rrule === rrule,
    error,
  };
}
