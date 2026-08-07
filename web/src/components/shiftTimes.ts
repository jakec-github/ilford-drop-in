// The hours a shift runs, as the rota and the availability form both say them.
//
// A shift's start and end are local wall clock at the drop-in (ADR 0007), so
// they are read off the string rather than through `new Date()`: parsing them
// as instants would re-render them in whatever zone the reader's phone is in,
// and a volunteer in another country would be told the wrong time.
function timeOfDay(timestamp: string): string {
  return timestamp.slice("2026-02-02T".length, "2026-02-02T19:30".length);
}

// "19:30–21:30", or "All day" for a shift running one midnight to the next.
//
// That second case is not a shift anybody typed: it is what the migration that
// made times mandatory left behind on a deployment where nobody had ever said
// when the drop-in runs. Rendering it as the day it is beats rendering
// "00:00–00:00", and an admin puts the real hours on it from the rota.
export function formatShiftTimes(start: string, end: string): string {
  const from = timeOfDay(start);
  const to = timeOfDay(end);
  if (from === "00:00" && to === "00:00") return "All day";
  return `${from}\u2013${to}`;
}
