import { useEffect, useRef, useState } from "react";
import type { SaveState } from "../hooks/useAutosave";
import { useRotaDefaults } from "../hooks/useRotaDefaults";
import SaveStatus from "./SaveStatus";
import SettingsSection from "./SettingsSection";

// The zones a browser knows, which are the IANA names the server loads.
function zonesWith(current: string): string[] {
  const zones = Intl.supportedValuesOf("timeZone");
  return current === "" || zones.includes(current)
    ? zones
    : [...zones, current].sort();
}

// The zone this browser is in, which is a better first guess than any fixed
// one: whoever opens Config first is almost always where the Organisation is.
function browserZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

// TimezoneForm is the zone as a picker. A zone nobody has chosen is set to
// the browser's the moment the page opens, so a pattern is never read in a
// zone nobody picked; changing it saves at once. A zone the server cannot load
// is refused, and said here.
export function TimezoneForm({
  zone,
  onSave,
}: {
  // "" when nobody has chosen one.
  zone: string;
  onSave: (zone: string) => Promise<void>;
}) {
  const [state, setState] = useState<SaveState>("idle");
  const [error, setError] = useState<string | null>(null);

  async function save(next: string) {
    setState("saving");
    setError(null);
    try {
      await onSave(next);
      setState("saved");
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to save the time zone",
      );
      setState("idle");
    }
  }

  // Once per mount, so a refused guess is said rather than retried.
  const guessed = useRef(false);
  useEffect(() => {
    if (zone !== "" || guessed.current) return;
    guessed.current = true;
    void save(browserZone());
  });

  return (
    <>
      <div className="settings-field">
        <label htmlFor="time-zone">Time zone</label>{" "}
        <select
          id="time-zone"
          value={zone}
          disabled={state === "saving"}
          onChange={(e) => void save(e.target.value)}
        >
          {zone === "" && <option value="">Not set</option>}
          {zonesWith(zone).map((z) => (
            <option key={z} value={z}>
              {z}
            </option>
          ))}
        </select>{" "}
        <SaveStatus state={state} />
      </div>
      {error && <p className="settings-error">{error}</p>}
    </>
  );
}

// TimezoneSetting is the zone the Organisation's times are read in: every
// time in the Shift pattern and the Overrides is wall-clock there, and the
// calendar feed tells a volunteer's calendar which zone that is (ADR 0014). It
// heads Config because everything below it is read in it.
export default function TimezoneSetting() {
  const { defaults, error, saveTimezone } = useRotaDefaults();
  return (
    <SettingsSection
      title="Time zone"
      blurb="Where the organisation runs. Shift times are wall-clock here, so 19:30 stays 19:30 when the clocks change."
    >
      {error && (
        <p className="settings-error">Could not load the settings: {error}</p>
      )}
      {defaults === null && !error && (
        <p className="settings-empty">Loading…</p>
      )}
      {defaults !== null && (
        <TimezoneForm zone={defaults.shiftTimezone} onSave={saveTimezone} />
      )}
    </SettingsSection>
  );
}
