import { useEffect, useState } from "react";
import { previewShiftPattern } from "../api";
import type { ShiftPatternPreview } from "../types";

// How long typing has to pause before the pattern is read again. Long enough
// that a line is not read once per keystroke, short enough that the reading
// keeps up with somebody checking what they wrote.
const SETTLE_MS = 300;

interface UseShiftPatternPreview {
  // The last answer, kept while a newer one is on its way so the descriptions
  // and the calendar do not blink out between keystrokes. null before the first.
  preview: ShiftPatternPreview | null;
  // A range or request the server could not read. A pattern with problems is
  // not an error: its problems are in preview.entries and preview.problems.
  error: string | null;
  // The preview is an answer to the pattern as it is now — not to an earlier
  // keystroke. False while a reading is on its way.
  current: boolean;
}

// useShiftPatternPreview reads a pattern that is still being written, between
// two dates: what each entry means, and where and for how long its Shifts run.
//
// The server does the reading, by the arithmetic that mints a rota (ADR 0014),
// rather than a second RRULE evaluator in the browser — two evaluators would be
// two answers to "when is the next shift", and the calendar would be the one
// that was wrong.
export function useShiftPatternPreview(
  shiftPattern: string,
  from: string,
  to: string,
): UseShiftPatternPreview {
  const [preview, setPreview] = useState<ShiftPatternPreview | null>(null);
  // What the last answer, preview or error, was an answer to.
  const [readFor, setReadFor] = useState("");
  const key = shiftPattern;
  const [error, setError] = useState<string | null>(null);

  // Nothing to read is answered without asking: no entries, no Shifts.
  const blank = shiftPattern.trim() === "";

  useEffect(() => {
    if (blank) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      previewShiftPattern(shiftPattern, from, to, controller.signal)
        .then((loaded) => {
          setPreview(loaded);
          setError(null);
          setReadFor(key);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted) return;
          setError(
            err instanceof Error ? err.message : "Failed to read the pattern",
          );
          setReadFor(key);
        });
    }, SETTLE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [blank, key, shiftPattern, from, to]);

  if (blank) {
    return {
      preview: {
        entries: [],
        problems: [],
        warnings: [],
        startsFrom: "",
        starts: [],
      },
      current: false,
      error: null,
    };
  }
  return { preview, current: readFor === key && error === null, error };
}
