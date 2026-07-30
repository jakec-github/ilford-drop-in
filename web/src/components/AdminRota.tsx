import { useState } from "react";
import Button from "../ui/Button";
import { useDefineRota } from "../hooks/useDefineRota";
import "./AdminRota.css";

const DEFAULT_SHIFT_COUNT = "6";

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

// AdminRota is the rota tab: define the next rota and see the shifts it minted.
//
// Defining is not idempotent — each call takes the weeks after the last rota — so
// the panel is built around showing what was just created rather than around
// preventing a second click. The count is left to the server to validate, so the
// message an admin reads is the one the API actually enforced.
export default function AdminRota() {
  const [shiftCount, setShiftCount] = useState(DEFAULT_SHIFT_COUNT);
  const { rota, error, defining, define } = useDefineRota();

  return (
    <section className="admin-panel define-rota">
      <h2>Rota</h2>
      <p className="define-rota-intro">
        A new rota starts the Sunday after the last one ends, with one shift a
        week. Defining again adds another rota after this one.
      </p>

      {/* noValidate deliberately: min below floors the spinner, but native
          validation would answer a bad count with a transient browser bubble in
          some cases and the server's message in others. Submitting regardless
          means one count is only ever rejected in one place, with one wording. */}
      <form
        className="define-rota-form"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void define(Number(shiftCount));
        }}
      >
        <label className="define-rota-field">
          Shifts
          <input
            type="number"
            inputMode="numeric"
            min="1"
            value={shiftCount}
            onChange={(e) => setShiftCount(e.target.value)}
          />
        </label>
        <Button type="submit" disabled={defining}>
          {defining ? "Defining…" : "Define rota"}
        </Button>
      </form>

      {/* aria-live so the outcome reaches a screen reader: submitting moves
          nothing into focus, so an unannounced result would go unnoticed. */}
      <div aria-live="polite">
        {error && (
          <p className="define-rota-error">Could not define: {error}</p>
        )}

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
  );
}
