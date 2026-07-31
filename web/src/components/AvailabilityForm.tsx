import Button from "../ui/Button";
import { useAvailabilityForm } from "../hooks/useAvailabilityForm";
import type { AvailabilityLinkFailure, AvailabilityShift } from "../types";
import "./AvailabilityForm.css";

// "Sunday 2 August" — the weekday matters more than the year here: a volunteer
// is checking which Sundays they can do, and every shift is within a couple of
// months.
function formatShiftDate(date: string): string {
  return new Date(date).toLocaleDateString("en-GB", {
    weekday: "long",
    day: "numeric",
    month: "long",
  });
}

// A link that will never work again gets its own screen, not an error banner
// over an empty form. The two reasons need different words: one asks the
// volunteer to check the link they followed, the other tells them the rota is
// already out and there is nothing to do.
const DEAD_LINK_MESSAGE: Record<AvailabilityLinkFailure, string> = {
  "not-found":
    "This availability link is not one we recognise. Check you followed the whole link from your email, or ask us for a new one.",
  gone: "The rota for these dates has already been worked out, so this link has closed. Get in touch if something has changed.",
};

// One shift, as a tick box. A closed date is shown and disabled rather than
// hidden, so a volunteer can see the drop-in is not running that week instead of
// wondering why a Sunday is missing.
function ShiftChoice({
  shift,
  checked,
  onToggle,
}: {
  shift: AvailabilityShift;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <li className={`shift-choice${shift.closed ? " shift-choice--closed" : ""}`}>
      <label>
        <input
          type="checkbox"
          checked={checked}
          disabled={shift.closed}
          onChange={onToggle}
        />
        <span className="shift-choice-date">{formatShiftDate(shift.date)}</span>
        {shift.closed && <span className="shift-choice-tag">Closed</span>}
      </label>
    </li>
  );
}

// AvailabilityForm is the volunteer's page: the one screen someone who is not an
// admin ever sees. Mobile first — it is opened from a phone, from an email.
//
// It lands with every open date already ticked, matching the Google form it
// replaces. That is deliberate and not just inherited: a mis-tap then records
// full availability, which is harmless, where starting blank would let a mis-tap
// record none at all — indistinguishable from a genuine "I can't do any of
// these", and enough to drop someone from the rota silently.
export default function AvailabilityForm({ token }: { token: string }) {
  const { form, deadLink, error, submitState, selected, toggle, submit } =
    useAvailabilityForm(token);

  if (deadLink) {
    return (
      <main className="availability">
        <h1>Availability</h1>
        <p className="availability-dead-link">{DEAD_LINK_MESSAGE[deadLink]}</p>
      </main>
    );
  }

  if (form === null) {
    return (
      <main className="availability">
        {error ? (
          <p className="availability-message availability-message--error">
            Could not load your form: {error}
          </p>
        ) : (
          <p className="app-status">Loading…</p>
        )}
      </main>
    );
  }

  const openShifts = form.shifts.filter((s) => !s.closed);
  const chosen = openShifts.filter((s) => selected.has(s.id)).length;

  return (
    <main className="availability">
      {/* The name is a check, not a greeting: a wrong one is how someone
          notices they are filling in a link forwarded to them. */}
      <h1>Availability for {form.volunteerName}</h1>

      {form.groupMembers.length > 0 && (
        <p className="availability-group">
          This also covers {form.groupMembers.join(" and ")} — you are rostered
          together.
        </p>
      )}

      <p className="availability-intro">
        Tick the dates you can do. Everything is ticked to start with, so untick
        the ones you cannot. You can come back to this link and change your
        answer any time before the rota is worked out.
      </p>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <ul className="shift-choices">
          {form.shifts.map((shift) => (
            <ShiftChoice
              key={shift.id}
              shift={shift}
              checked={selected.has(shift.id)}
              onToggle={() => toggle(shift.id)}
            />
          ))}
        </ul>

        <p className="availability-count">
          {chosen} of {openShifts.length} dates ticked
        </p>

        <Button type="submit" disabled={submitState === "sending"}>
          {submitState === "sending" ? "Sending…" : "Send my availability"}
        </Button>

        {/* aria-live because submitting moves nothing into focus: without it
            the outcome would never reach a screen reader. */}
        <div aria-live="polite">
          {submitState === "sent" && (
            <p className="availability-message availability-message--ok">
              Thank you — we have your answer.
            </p>
          )}
          {submitState === "error" && error && (
            <p className="availability-message availability-message--error">
              We could not send that: {error}
            </p>
          )}
          {/* Shown only before the first send of this visit, so someone
              returning to the link knows their earlier answer is what stands. */}
          {submitState === "idle" && form.submitted && (
            <p className="availability-message">
              You have already answered. This form shows what you told us — send
              again to change it.
            </p>
          )}
        </div>
      </form>
    </main>
  );
}
