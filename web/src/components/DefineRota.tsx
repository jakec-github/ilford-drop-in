import { useState } from "react";
import Button from "../ui/Button";
import { useDefineRota } from "../hooks/useDefineRota";
import RotaDefaultsCard from "./RotaDefaultsCard";
import type { NewRota, RotaProposal } from "../types";
import "./DefineRota.css";

// "Sun 2 Aug 2026" — the weekday is worth showing here, unlike on the rota
// itself: what an admin is checking is that the weeks they expected were taken.
function formatShiftDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

// DefineRotaForm is the define screen's form: how many shifts, and from when.
//
// It used to carry the hours and the Shape as well, each prefilled from the
// Rota Defaults and each a field of this rota alone (issue #140). They have
// gone (issue #176). Two places to state the same thing was one too many: an
// admin who set the hours here was surprised to find the settings unchanged,
// and one who set them on Settings was surprised to find them changeable here.
// The Rota Defaults card below the form is now the only place either is said,
// and defining spends whatever it shows.
//
// What is left is the two answers that are nobody's setting. The start date is
// the one an admin is most likely to touch — a rota can begin after a break
// rather than the week after the last one.
//
// It is mounted on a loaded proposal and never sees a null one, which is what
// lets the date initialise from it directly: a re-read unmounts the form rather
// than trying to reconcile a new answer with what somebody has typed.
function DefineRotaForm({
  proposal,
  defining,
  onDefine,
}: {
  proposal: RotaProposal;
  defining: boolean;
  onDefine: (rota: NewRota) => void;
}) {
  // Empty, unlike the date. That starts from the proposal because there is a
  // right answer to start from; how long the next rota should run is a decision
  // nobody has made yet, and a number already in the box is one an admin can
  // define a rota without ever reading (issue #174).
  const [shiftCount, setShiftCount] = useState("");
  const [startDate, setStartDate] = useState(proposal.startDate);

  return (
    <>
      <p className="define-rota-intro">
        One shift a week from the date below, each running the hours the Rota
        Defaults state. Once the rota is defined it is the rota in flight, and
        this screen becomes the place it is prepared, asked about and allocated.
      </p>

      {/* noValidate deliberately: min below floors the spinner, but native
          validation would answer a bad count with a transient browser bubble in
          some cases and the server's message in others. Submitting regardless
          means one field is only ever rejected in one place, with one
          wording. */}
      <form
        className="define-rota-form"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          onDefine({ shiftCount: Number(shiftCount), startDate });
        }}
      >
        <div className="define-rota-fields">
          <label className="define-rota-field">
            Shifts
            <input
              className="define-rota-count"
              type="number"
              inputMode="numeric"
              min="1"
              value={shiftCount}
              onChange={(e) => setShiftCount(e.target.value)}
            />
          </label>

          <label className="define-rota-field">
            First shift
            <input
              type="date"
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
            />
          </label>
        </div>

        <div className="define-rota-actions">
          <Button type="submit" disabled={defining}>
            {defining ? "Defining…" : "Define rota"}
          </Button>
        </div>
      </form>

      <p className="define-rota-note">
        Every shift is minted with the times and the shape below, as they stand
        when the rota is defined. Changing them afterwards changes what the next
        rota is made from; this one keeps what it was made with, and each of its
        shifts can be changed on its own.
      </p>
    </>
  );
}

// DefineRota is the Allocation tab with nothing in flight: the form that starts
// the next rota, and the settings that rota will be made from.
//
// The Rota Defaults card is the same one the settings screen shows (issue
// #176). It is here because this is the moment those settings are spent: an
// admin about to define a rota is exactly the person who needs to see what its
// shifts will run, and to fix it without leaving the screen if it is wrong.
//
// It used to carry a pointer to the rota already out under the form. That has
// gone (issue #174): the rota page is one click away in the header and has far
// more to say about an allocated rota than a date could, so the line was a
// second place to look rather than an answer.
//
// Everything else the tab does — preparing the shifts, the round, the draft,
// allocating — is about a rota that exists, and none of it can be done to one
// that does not.
export default function DefineRota({
  onDefined,
}: {
  // Called after a define that may have worked. The tab re-reads what is in
  // flight, which is what swaps this screen for the working one.
  onDefined: () => void;
}) {
  const { proposal, rota, error, defining, define } = useDefineRota();

  return (
    <>
      <section className="admin-panel define-rota">
        <h2>Define the next rota</h2>

        {proposal === null && !error && (
          <p className="define-rota-intro">Working out the next rota…</p>
        )}

        {proposal !== null && (
          <DefineRotaForm
            proposal={proposal}
            defining={defining}
            onDefine={(next) => {
              // Reloading whatever the outcome: a define that succeeded put a
              // rota in flight, and one that was refused most likely means one
              // already was.
              void define(next).then(onDefined);
            }}
          />
        )}

        {/* aria-live so the outcome reaches a screen reader: submitting moves
            nothing into focus, so an unannounced result would go unnoticed. */}
        <div aria-live="polite">
          {error && (
            <p className="define-rota-error">Could not define: {error}</p>
          )}

          {/* Only until the tab re-reads and swaps this whole screen for the
              working one, which is the real confirmation. Worth showing in the
              meantime: the dates are the one thing the form did not state
              outright, and a shift landing on a day nobody expected is easiest
              to see here. */}
          {rota && (
            <>
              <p className="define-rota-result">
                Defined {rota.shiftDates.length}{" "}
                {rota.shiftDates.length === 1 ? "shift" : "shifts"}:
              </p>
              <ol className="define-rota-dates">
                {rota.shiftDates.map((date) => (
                  <li key={date}>{formatShiftDate(date)}</li>
                ))}
              </ol>
            </>
          )}
        </div>
      </section>

      <RotaDefaultsCard />
    </>
  );
}
