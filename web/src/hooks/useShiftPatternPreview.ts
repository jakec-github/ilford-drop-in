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
  // not an error: its problems are in preview.lines.
  error: string | null;
}

// useShiftPatternPreview reads a pattern that is still being written, between
// two dates: what each line means, and where its Shifts start.
//
// The server does the reading, by the arithmetic that mints a rota (ADR 0013),
// rather than a second RRULE evaluator in the browser — two evaluators would be
// two answers to "when is the next shift", and the calendar would be the one
// that was wrong.
export function useShiftPatternPreview(
  shiftPattern: string,
  patternStartFrom: string,
  shiftTimezone: string,
  from: string,
  to: string,
): UseShiftPatternPreview {
  const [preview, setPreview] = useState<ShiftPatternPreview | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Nothing to read is answered without asking: no lines, no Shifts.
  const blank = shiftPattern.trim() === "";

  useEffect(() => {
    if (blank) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      previewShiftPattern(
        { shiftPattern, patternStartFrom, shiftTimezone },
        from,
        to,
        controller.signal,
      )
        .then((loaded) => {
          setPreview(loaded);
          setError(null);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted) return;
          setError(
            err instanceof Error ? err.message : "Failed to read the pattern",
          );
        });
    }, SETTLE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [blank, shiftPattern, patternStartFrom, shiftTimezone, from, to]);

  if (blank) {
    return { preview: { patternStartFrom, lines: [], starts: [] }, error: null };
  }
  return { preview, error };
}
