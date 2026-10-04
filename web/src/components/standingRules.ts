// Which Shifts of a rota a Standing Preallocation lands on, offered as the
// handful of answers anybody actually gives. Each is a recurrence rule, which is
// what the server stores and what the seeding matches shift dates against; an
// Organiser picks the sentence and never sees the rule.
//
// Sundays because that is the day a rota is minted on — definition walks weekly
// from a Sunday. A cadence that is not weekly is out of scope until rota
// definition offers one.
export const STANDING_RULES: { rrule: string; label: string }[] = [
  { rrule: "FREQ=WEEKLY;BYDAY=SU", label: "Every shift" },
  { rrule: "FREQ=MONTHLY;BYDAY=1SU", label: "The first Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=2SU", label: "The second Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=3SU", label: "The third Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=4SU", label: "The fourth Sunday of the month" },
  { rrule: "FREQ=MONTHLY;BYDAY=-1SU", label: "The last Sunday of the month" },
];

// How a stored rule reads in the list. A rule this app did not offer — written
// against the API, or offered by an older build — falls back to itself rather
// than to nothing: it is still doing something, and an Organiser deciding whether to
// remove it needs to see what.
export function describeRule(rrule: string): string {
  return STANDING_RULES.find((r) => r.rrule === rrule)?.label ?? rrule;
}
