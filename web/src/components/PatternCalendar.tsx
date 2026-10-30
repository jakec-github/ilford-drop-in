import { endOf, formatDay, type CalendarMonth } from "./patternDates";
import type { PatternStart } from "../types";
import "./PatternCalendar.css";

// PatternCalendar is a Shift pattern drawn as months: a dot on every day a
// Shift starts, one per Shift up to four, and the times on hover or focus.
//
// It draws what the server generated and works nothing out itself — the starts
// come from the same evaluator that mints a rota (ADR 0014), so a time shown
// here is the time defining would give that Shift. Only the end is added up
// here, from the length of the entry that made it, and that is a sum rather
// than a rule.
export default function PatternCalendar({
  months,
  starts,
  startsFrom,
  today,
  moved,
  onMove,
  onHome,
}: {
  months: CalendarMonth[];
  // Wall-clock starts, "2026-11-01T19:30:00", in order, each with its length.
  starts: PatternStart[];
  // The earliest entry's date: nothing is minted before it.
  startsFrom: string;
  today: string;
  // Whether the months shown are not the ones the pattern starts in.
  moved: boolean;
  onMove: (months: number) => void;
  onHome: () => void;
}) {
  const byDay = new Map<string, PatternStart[]>();
  for (const start of starts) {
    const day = start.start.slice(0, 10);
    const times = byDay.get(day) ?? [];
    times.push(start);
    byDay.set(day, times);
  }

  return (
    <div className="pattern-calendar">
      <div className="pattern-calendar-nav">
        <button
          type="button"
          className="pattern-calendar-step"
          aria-label="Earlier months"
          onClick={() => onMove(-3)}
        >
          ‹
        </button>
        <button
          type="button"
          className="pattern-calendar-step"
          aria-label="Later months"
          onClick={() => onMove(3)}
        >
          ›
        </button>
        {moved && (
          <button
            type="button"
            className="pattern-calendar-home"
            onClick={onHome}
          >
            Back to where it starts
          </button>
        )}
        <span className="pattern-calendar-note">
          A dot is a shift. Hover over or tab to a date for its times.
        </span>
      </div>

      <div className="pattern-calendar-months">
        {months.map((month) => (
          <div key={month.key} className="pattern-calendar-month">
            <h4>{month.label}</h4>
            <div className="pattern-calendar-grid">
              {["M", "T", "W", "T", "F", "S", "S"].map((d, i) => (
                <span key={i} className="pattern-calendar-dow" aria-hidden>
                  {d}
                </span>
              ))}
              {Array.from({ length: month.lead }, (_, i) => (
                <span key={`lead-${i}`} />
              ))}
              {month.days.map((day) => {
                const times = byDay.get(day) ?? [];
                const classes = ["pattern-calendar-day"];
                if (times.length > 0) classes.push("on");
                if (day < startsFrom) classes.push("before");
                if (day === today) classes.push("today");
                const dayNumber = Number(day.slice(8));
                if (times.length === 0) {
                  return (
                    <span key={day} className={classes.join(" ")}>
                      {dayNumber}
                    </span>
                  );
                }
                const spans = times.map((start) => {
                  const end = endOf(start.start, start.lengthMinutes);
                  return {
                    start: start.start.slice(11, 16),
                    end: end.time,
                    nextDay: end.nextDay,
                  };
                });
                return (
                  <span
                    key={day}
                    className={classes.join(" ")}
                    tabIndex={0}
                    aria-label={`${formatDay(day)}: ${spans.map((s) => s.start).join(", ")}`}
                  >
                    {dayNumber}
                    <span className="pattern-calendar-dots" aria-hidden>
                      {spans.slice(0, 4).map((s) => (
                        <i key={s.start} />
                      ))}
                    </span>
                    <span className="pattern-calendar-tip" aria-hidden>
                      <b>{formatDay(day)}</b>
                      {spans.map((s) => (
                        <span key={s.start}>
                          {s.start}–{s.end}
                          {s.nextDay && <small> (next day)</small>}
                        </span>
                      ))}
                    </span>
                  </span>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
