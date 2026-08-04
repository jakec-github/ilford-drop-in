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

// One shift, as a no/yes pair. Radios rather than a tick box because no is an
// answer someone gives, not the absence of one: on a tick box an empty row reads
// the same whether the volunteer decided against that Sunday or never got to it,
// and both of them look like "no" by the time the rota is worked out.
//
// Each radio carries the date in its own accessible name. The visible word is
// only "No" or "Yes", which says nothing on its own to anyone who cannot see
// which row it sits in.
//
// A closed date offers no answer at all — the drop-in is not running, so there
// is nothing to say — but it is still listed, so a volunteer can see that
// instead of wondering why a Sunday is missing.
//
// A row they have moved since their last send is marked, because the answer it
// is replacing has gone from the screen: without the mark, a volunteer who came
// back to change one Sunday has nothing to check their edit against before
// sending it.
function ShiftChoice({
  shift,
  available,
  changed,
  onAnswer,
}: {
  shift: AvailabilityShift;
  available: boolean;
  // True when this answer differs from the one already sent. Never set before a
  // first send, so the note below can talk about what they told us without
  // qualifying it.
  changed: boolean;
  onAnswer: (available: boolean) => void;
}) {
  const date = formatShiftDate(shift.date);

  return (
    <li
      className={[
        "shift-choice",
        shift.closed && "shift-choice--closed",
        changed && "shift-choice--changed",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <span className="shift-choice-label">
        <span className="shift-choice-date">{date}</span>
        {/* A change is only legible against something that is no longer on
            screen, so the row has to say what it was. Text, not just the accent
            bar: colour on its own reaches nobody who cannot see it. */}
        {changed && (
          <span className="shift-choice-was">
            Changed from {available ? "no" : "yes"}
          </span>
        )}
      </span>
      {shift.closed ? (
        <span className="shift-choice-tag">Closed</span>
      ) : (
        <span className="shift-answer">
          <label className="shift-option">
            <input
              type="radio"
              name={`shift-${shift.id}`}
              checked={!available}
              onChange={() => onAnswer(false)}
              aria-label={`No, I cannot do ${date}`}
            />
            No
          </label>
          <label className="shift-option">
            <input
              type="radio"
              name={`shift-${shift.id}`}
              checked={available}
              onChange={() => onAnswer(true)}
              aria-label={`Yes, I can do ${date}`}
            />
            Yes
          </label>
        </span>
      )}
    </li>
  );
}

// AvailabilityForm is the volunteer's page: the one screen someone who is not an
// admin ever sees. Mobile first — it is opened from a phone, from an email.
//
// It lands with every open date already on yes, matching the Google form it
// replaces. That is deliberate and not just inherited: a mis-tap then records
// full availability, which is harmless, where starting on no would let a mis-tap
// record none at all — indistinguishable from a genuine "I can't do any of
// these", and enough to drop someone from the rota silently (ADR 0004).
export default function AvailabilityForm({ token }: { token: string }) {
  const {
    form,
    deadLink,
    error,
    submitState,
    selected,
    changed,
    setAvailable,
    submit,
  } = useAvailabilityForm(token);

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

      {/* Two different situations, and the first sentence is what tells them
          which one they are in: a blank form to fill in, or the answer they
          already sent. Someone who has answered is not being asked again. */}
      <p className="availability-intro">
        {form.submitted ? (
          <>
            This is the answer you sent us. Change any date and send again —
            what you change is marked, so you can check it before it goes. You
            can do this any time before the rota is worked out.
          </>
        ) : (
          <>
            Answer yes or no for each date. Every date starts on yes, so change
            the ones you cannot do. You can come back to this link and change
            your answer any time before the rota is worked out.
          </>
        )}
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
              available={selected.has(shift.id)}
              changed={changed.has(shift.id)}
              onAnswer={(available) => setAvailable(shift.id, available)}
            />
          ))}
        </ul>

        <p className="availability-count">
          Yes to {chosen} of {openShifts.length} dates
          {/* The marks up the list say which dates moved; this says how many,
              which is the bit that is hard to hold in your head once the list is
              longer than the screen. "Not sent" is the point of it — the edit
              lives in the browser until the button is pressed. */}
          {changed.size > 0 && (
            <span className="availability-count-changed">
              {changed.size === 1
                ? "1 date changed"
                : `${changed.size} dates changed`}
              , not sent yet
            </span>
          )}
        </p>

        <Button type="submit" disabled={submitState === "sending"}>
          {submitState === "sending"
            ? "Sending…"
            : changed.size > 0
              ? "Send my changes"
              : "Send my availability"}
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
        </div>
      </form>
    </main>
  );
}
