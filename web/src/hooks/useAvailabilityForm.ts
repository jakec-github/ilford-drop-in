import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AvailabilityLinkError,
  fetchAvailabilityForm,
  submitAvailability,
} from "../api";
import type { AvailabilityFormState, AvailabilityLinkFailure } from "../types";

export type SubmitState = "idle" | "sending" | "sent" | "error";

interface UseAvailabilityForm {
  // null while the first load is in flight.
  form: AvailabilityFormState | null;
  // Set when the link itself will never work, which is a different screen from
  // a request that happened to fail.
  deadLink: AvailabilityLinkFailure | null;
  error: string | null;
  submitState: SubmitState;
  selected: Set<string>;
  // Shifts whose answer on screen differs from the one the server holds. Empty
  // until they have answered once — before that the form's yeses are a default
  // nobody chose, so calling them changes would be inventing an earlier answer.
  changed: ReadonlySet<string>;
  // Set, not toggle: the form asks yes or no per shift, and answering "no" to a
  // shift already at no must leave it there rather than flip it to yes.
  setAvailable: (shiftId: string, available: boolean) => void;
  submit: () => Promise<void>;
}

const NOTHING_CHANGED: ReadonlySet<string> = new Set();

// useAvailabilityForm owns one volunteer's form: the load, the answers they have
// given since, and the send.
//
// The selection is held here rather than derived from `form` on every render
// because the two diverge the moment someone answers no — `form` is what the
// server last confirmed, `selected` is what they are about to say. Submitting
// reconciles them, so a successful send leaves no local state pretending to be
// server state.
export function useAvailabilityForm(token: string): UseAvailabilityForm {
  const [form, setForm] = useState<AvailabilityFormState | null>(null);
  const [deadLink, setDeadLink] = useState<AvailabilityLinkFailure | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [submitState, setSubmitState] = useState<SubmitState>("idle");
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const adopt = useCallback((loaded: AvailabilityFormState) => {
    setForm(loaded);
    setSelected(new Set(loaded.selectedShiftIds));
    setError(null);
  }, []);

  useEffect(() => {
    let current = true;
    fetchAvailabilityForm(token)
      .then((loaded) => {
        if (current) adopt(loaded);
      })
      .catch((err: unknown) => {
        if (!current) return;
        if (err instanceof AvailabilityLinkError) {
          setDeadLink(err.reason);
          return;
        }
        setError(
          err instanceof Error ? err.message : "Failed to load the form",
        );
      });
    return () => {
      current = false;
    };
  }, [token, adopt]);

  // Answering after a send puts the form back into an unsent state: what is on
  // screen is no longer what the server holds, and saying "sent" over an edited
  // form would be a lie.
  const setAvailable = useCallback((shiftId: string, available: boolean) => {
    setSubmitState("idle");
    setSelected((previous) => {
      if (previous.has(shiftId) === available) return previous;
      const next = new Set(previous);
      if (available) next.add(shiftId);
      else next.delete(shiftId);
      return next;
    });
  }, []);

  // `form` is the last thing the server confirmed, so it doubles as the answer
  // being edited away from — and a send adopts the response, which is what makes
  // the highlighting clear itself rather than persisting over a saved form.
  // Closed shifts are skipped: they carry no answer either side of the
  // comparison.
  const changed = useMemo(() => {
    if (form === null || !form.submitted) return NOTHING_CHANGED;
    const answered = new Set(form.selectedShiftIds);
    const differing = new Set<string>();
    for (const shift of form.shifts) {
      if (shift.closed) continue;
      if (answered.has(shift.id) !== selected.has(shift.id)) {
        differing.add(shift.id);
      }
    }
    return differing;
  }, [form, selected]);

  const submit = useCallback(async () => {
    setSubmitState("sending");
    try {
      // The whole selection every time, never just what changed: an absent
      // shift is a no, so a partial send would record unavailability.
      adopt(await submitAvailability(token, [...selected]));
      setSubmitState("sent");
    } catch (err: unknown) {
      if (err instanceof AvailabilityLinkError) {
        setDeadLink(err.reason);
        setSubmitState("idle");
        return;
      }
      setError(err instanceof Error ? err.message : "Failed to send");
      setSubmitState("error");
    }
  }, [token, selected, adopt]);

  return {
    form,
    deadLink,
    error,
    submitState,
    selected,
    changed,
    setAvailable,
    submit,
  };
}
