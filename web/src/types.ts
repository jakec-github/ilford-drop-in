// Role a person holds on a given shift. Only these two exist; anything the API
// doesn't recognise as team lead is treated as a service volunteer.
export type Role = "lead" | "volunteer";

// Assignee is one person on a shift: a real volunteer or a custom (manual)
// entry. Role is the role held on this shift, not the volunteer's intrinsic
// role. Group is the volunteer's group key, or null for custom/ungrouped.
// volunteerId is the real volunteer's id, or null for custom entries; it keys
// their ICS calendar feed.
export interface Assignee {
  name: string;
  role: Role;
  custom: boolean;
  group: string | null;
  volunteerId: string | null;
}

// Volunteer is one entry of the admin roster. Role is the volunteer's intrinsic
// role, unlike Assignee.role which is the role held on one shift. Group is their
// group key, or null when they are not in one.
//
// name is the display name — the shortest unambiguous form, as the rota shows it.
// fullName is first plus last, for screens with room for it.
//
// gender is free text as recorded on the roster sheet, so it is shown as-is and
// null when nothing was recorded — never inferred. active is false for someone
// who has stopped volunteering: the roster lists them rather than hiding them.
export interface Volunteer {
  id: string;
  name: string;
  fullName: string;
  role: Role;
  group: string | null;
  gender: string | null;
  active: boolean;
}

// DefinedRota is a rota that has just been defined: the span it covers and the
// dates of the shifts it minted, in order. Returned by the define call so the
// admin can see what they created — defining is not idempotent, so what came
// back is the only confirmation of which weeks were taken.
export interface DefinedRota {
  id: string;
  start: string;
  end: string;
  shiftDates: string[];
}

// PersonRef identifies someone on a shift for the purpose of changing it. A
// real volunteer is keyed by id; a custom (manual) entry has none, so it is
// keyed by the text itself — which is also how the API removes one.
export type PersonRef = { volunteerId: string } | { custom: string };

// RotaChange is one change to a published rota, mirroring what POST /alterations
// accepts. Every operation the rota page offers is one of these:
//
//   add     { date, in }
//   remove  { date, out }
//   replace { date, out: leaving, in: arriving }
//   move    { date: destination, in: person, swapDate: where they were }
//   swap    { date: A's shift, out: A, in: B, swapDate: B's shift }
//
// swapDate applies the same change reversed on a second date, which is what
// makes move and swap a single atomic request rather than two.
//
// role sets the role the incoming volunteer takes; omitted, the server infers
// it. It cannot be combined with swapDate, where each date has its own incoming
// person, and team lead is refused where the shift already has one. reason is
// mandatory — the change is recorded against it.
export interface RotaChange {
  date: string;
  in?: PersonRef;
  out?: PersonRef;
  swapDate?: string;
  role?: Role;
  reason: string;
}

// AvailabilityShift is one of a rota's shifts as the availability screens show
// it. Closed shifts are carried rather than dropped: a volunteer seeing the date
// listed and shut knows the drop-in is not running, where a missing date just
// looks like a mistake.
export interface AvailabilityShift {
  id: string;
  date: string;
  closed: boolean;
}

// AvailabilityEntry is one volunteer's place in a round, as an admin sees it.
//
// link is the volunteer's whole URL, ready to copy — distribution is
// copy-the-link until sending is built, and stays the fallback when an email
// bounces. coveredBy names group partners who have already answered: a group
// answers as a unit, so a volunteer whose partner replied is covered rather than
// missing, and chasing them would be chasing an answer we already have.
export interface AvailabilityEntry {
  volunteerId: string;
  volunteerName: string;
  link: string;
  // When their link was emailed, or null while it has not been. Minting and
  // sending are separate operations, so holding a link nobody has sent is an
  // ordinary state — and it is the one a round send acts on.
  sentAt: string | null;
  replied: boolean;
  submittedAt: string | null;
  availableShiftIds: string[];
  coveredBy: string[];
}

// Which emails a send covers, and what they say. The server owns the selection
// rules; these are the names it answers to.
export type SendMode = "round" | "reminder" | "resend";

// One volunteer a send reached, or failed to. error is what makes it a failure —
// a bounced address, or a volunteer with no address at all.
export interface SendOutcome {
  volunteerId: string;
  volunteerName: string;
  email: string | null;
  error: string | null;
}

// AvailabilitySend is one send in flight or just finished.
//
// A send is watched rather than awaited because it takes about ninety seconds:
// Gmail is throttled to one email every three seconds, and the browser arrives
// back from the consent screen long before the last email goes out. done of
// total is what fills the gap.
export interface AvailabilitySend {
  id: string;
  mode: SendMode;
  done: number;
  total: number;
  finished: boolean;
  sent: SendOutcome[];
  failed: SendOutcome[];
  error: string | null;
}

// AvailabilityGroup is a round at the grain allocation happens at: the people
// placed together, and the one answer that speaks for them.
//
// availableShiftIds is the group rule already applied by the server — the
// intersection over whoever answered — so nothing here re-derives it. Empty for
// a group nobody has answered for, which replied is what tells apart from a
// group that answered "none of these".
export interface AvailabilityGroup {
  key: string;
  name: string;
  replied: boolean;
  availableShiftIds: string[];
  members: AvailabilityEntry[];
}

// ShiftCoverage is one shift's staffing picture before the rota is run: what it
// still needs once already-pinned seats are taken out, how many people are
// available to fill them, and whether it has a team lead. delta is the number an
// admin is really after — negative is short.
//
// A closed shift carries zeroes: the drop-in is not running that day, so it is
// not a shift that is short of people.
export interface ShiftCoverage {
  id: string;
  date: string;
  closed: boolean;
  needed: number;
  pinned: number;
  available: number;
  delta: number;
  hasTeamLead: boolean;
}

// AvailabilityRound is a rota's round: how each of its shifts is looking, and
// where everyone asked has got to. allocated means the round is closed — the
// links have stopped working, so an admin looking at it is reading history.
export interface AvailabilityRound {
  rotaId: string;
  start: string;
  end: string;
  allocated: boolean;
  shifts: ShiftCoverage[];
  groups: AvailabilityGroup[];
}

// AvailabilityFormState is what a volunteer sees behind their link.
//
// selectedShiftIds is the form's landing state, not a record: before a first
// submission it holds every open shift, because the model is opt-out. Afterwards
// it holds what they last said, so re-opening the link shows their answer.
export interface AvailabilityFormState {
  volunteerName: string;
  groupMembers: string[];
  shifts: AvailabilityShift[];
  selectedShiftIds: string[];
  submitted: boolean;
  submittedAt: string | null;
}

// Why a link stopped working, kept apart because they mean different things to
// the person holding it: "this was never a link" versus "you are too late".
export type AvailabilityLinkFailure = "not-found" | "gone";

export interface RotaShift {
  date: string;
  closed: boolean;
  // allocated is false for a minted shift whose rota has not been run yet: it
  // exists but has no assignees. Shown only to admins (with a distinct style).
  allocated: boolean;
  assignees: Assignee[];
}

// Where a preallocation came from. "config" pins are written into the server's
// rota overrides and can only be changed by editing that file; "manual" pins are
// recorded against a single shift over the API. Both are guarantees the
// allocator has to honour, so both are worth showing, but only one of them is
// something an admin can undo from here.
export type PreallocationSource = "config" | "manual";

// Preallocation is one person the allocator is already committed to placing on a
// shift, before allocation has run. name is what to show — a volunteer's display
// name, or a custom entry (an outside group, say) verbatim.
//
// id addresses the stored pin for a delete, and is null for config pins, which
// have no row behind them.
export interface Preallocation {
  id: string | null;
  date: string;
  role: Role;
  name: string;
  custom: boolean;
  volunteerId: string | null;
  source: PreallocationSource;
}

// NewPreallocation is one person to pin to a shift the rota has not been run
// for. Always a manual pin — config pins are written into the server's own
// config, never over the API.
//
// role is what they are pinned as. "lead" is accepted only for a volunteer the
// roster records as a team lead, and never for a custom entry, which the API
// gives no role to.
export interface NewPreallocation {
  date: string;
  person: PersonRef;
  role: Role;
}
