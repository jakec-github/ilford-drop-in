// Dates and times as the Shift pattern screens say them.
//
// Every date here is a calendar date, "2026-11-01", and every time a wall-clock
// one at the Organisation (ADR 0007), so nothing is read through the browser's
// own zone: a date is pinned to UTC to be formatted, and a time is read off the
// string. Nothing here works out when a Shift is — the server does that — only
// how to say it.

const DAY_MS = 24 * 60 * 60 * 1000;

function utcDate(date: string): Date {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d));
}

export function isDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  return toDate(utcDate(value)) === value;
}

function toDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

// "Sun 1 Nov 2026". Put together by hand rather than by toLocaleDateString,
// whose en-GB reading has gained a comma after the weekday in some versions of
// ICU and not others: the calendar's labels are what tests and screen readers
// find a day by, so they say one thing everywhere.
export function formatDay(date: string): string {
  const d = utcDate(date);
  return `${WEEKDAYS[d.getUTCDay()]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}

// "Sun 1 Nov 2026 at 19:30", for a wall-clock timestamp.
export function formatStart(timestamp: string): string {
  return `${formatDay(timestamp.slice(0, 10))} at ${timestamp.slice(11, 16)}`;
}

export function addDays(date: string, days: number): string {
  return toDate(new Date(utcDate(date).getTime() + days * DAY_MS));
}

// The date in the browser's own zone today. Only ever a default, or the ring
// on today's square in the calendar: the server settles what today is for
// anything saved.
export function today(): string {
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

// The lengths offered for a Shift, a quarter of an hour apart up to sixteen
// hours. The server takes anything from a minute to a day; these are the ones
// anybody runs, and a length saved outside them is offered too rather than
// lost.
const SHIFT_LENGTHS = Array.from({ length: 64 }, (_, i) => (i + 1) * 15);

export function lengthChoices(current: number): number[] {
  return SHIFT_LENGTHS.includes(current)
    ? SHIFT_LENGTHS
    : [...SHIFT_LENGTHS, current].sort((a, b) => a - b);
}

// "2 hours", "45 minutes", "2 hours 15 minutes".
export function formatLength(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  const parts: string[] = [];
  if (hours > 0) parts.push(`${hours} ${hours === 1 ? "hour" : "hours"}`);
  if (rest > 0) parts.push(`${rest} ${rest === 1 ? "minute" : "minutes"}`);
  return parts.join(" ");
}

// When a Shift starting at `timestamp` and running `minutes` ends: the time of
// day, and whether that is on a later day than it started.
export function endOf(
  timestamp: string,
  minutes: number,
): { time: string; nextDay: boolean } {
  const startMinute =
    Number(timestamp.slice(11, 13)) * 60 + Number(timestamp.slice(14, 16));
  const end = startMinute + minutes;
  const m = end % 1440;
  const pad = (n: number) => String(n).padStart(2, "0");
  return {
    time: `${pad(Math.floor(m / 60))}:${pad(m % 60)}`,
    nextDay: end >= 1440,
  };
}

export interface CalendarMonth {
  // "2026-11"
  key: string;
  // "November 2026"
  label: string;
  // The first of the month, and the first of the month after.
  first: string;
  next: string;
  // How many blank squares come before the first, Monday first.
  lead: number;
  days: string[];
}

// The `count` months starting `offset` months after the month `date` is in.
export function monthsFrom(
  date: string,
  offset: number,
  count: number,
): CalendarMonth[] {
  const anchor = utcDate(date);
  const months: CalendarMonth[] = [];
  for (let k = 0; k < count; k++) {
    const first = new Date(
      Date.UTC(anchor.getUTCFullYear(), anchor.getUTCMonth() + offset + k, 1),
    );
    const next = new Date(
      Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 1),
    );
    const days: string[] = [];
    for (let t = first.getTime(); t < next.getTime(); t += DAY_MS) {
      days.push(toDate(new Date(t)));
    }
    months.push({
      key: toDate(first).slice(0, 7),
      label: first.toLocaleDateString("en-GB", {
        month: "long",
        year: "numeric",
        timeZone: "UTC",
      }),
      first: toDate(first),
      next: toDate(next),
      lead: (first.getUTCDay() + 6) % 7,
      days,
    });
  }
  return months;
}
