import type {
  AllocationSettings,
  AvailabilityEntry,
  AvailabilityFormState,
  AvailabilityLinkFailure,
  AvailabilityRound,
  AvailabilitySend,
  ConfiguredRole,
  DefinedRota,
  NewPreallocation,
  NewStandingPreallocation,
  PersonRef,
  Preallocation,
  RoleColour,
  RoleEdit,
  RotaChange,
  RotaDefaults,
  RotaInFlight,
  RotaShift,
  SendMode,
  SendOutcome,
  ShapeSeat,
  ShiftTimes,
  StandingPreallocation,
  Volunteer,
} from "./types";
import {
  DEFAULT_ROLE_COLOUR,
  ROLE_COLOURS,
  SERVICE_VOLUNTEER_ROLE,
} from "./types";

interface ApiAssignee {
  volunteerId?: string;
  customEntry?: string;
  name: string;
  role?: string;
  group?: string;
}

interface ApiShift {
  id: string;
  date: string;
  start: string;
  end: string;
  closed: boolean;
  allocated: boolean;
  // What the shift asks for. Absent from a server that predates per-shift
  // Shapes, which reads as a shift asking for nobody — the same thing the
  // server says with an empty list.
  shape?: ShapeSeat[];
  assignees: ApiAssignee[];
}

interface ListShiftsResponse {
  shifts: ApiShift[];
}

interface ApiVolunteer {
  id: string;
  name: string;
  fullName: string;
  roles: string[];
  group?: string;
  gender?: string;
  active: boolean;
}

interface ListVolunteersResponse {
  volunteers: ApiVolunteer[];
}

interface DefineRotaResponse {
  rotation: { id: string; start: string; end: string; shiftCount: number };
  shifts: { id: string; date: string }[];
}

// The API reports a rejected request as {"error": "..."}, and that message is
// written to be read — "shift count must be positive, got 0" tells an admin what
// to change, where a bare 400 does not. Falls back to the status when the body
// is not one of ours.
async function errorMessage(res: Response, fallback: string): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    // Not a JSON error body; the status is all we have.
  }
  return `${fallback} (${res.status})`;
}

function toVolunteer(v: ApiVolunteer): Volunteer {
  return {
    id: v.id,
    name: v.name,
    fullName: v.fullName,
    // In the order the API sends them: highest-priority Role first.
    roles: v.roles,
    group: v.group || null,
    gender: v.gender || null,
    active: v.active,
  };
}

function toRotaShift(shift: ApiShift): RotaShift {
  return {
    id: shift.id,
    date: shift.date,
    start: shift.start,
    end: shift.end,
    closed: shift.closed,
    allocated: shift.allocated,
    shape: shift.shape ?? [],
    // Closed shifts carry no meaningful assignees.
    assignees: shift.closed
      ? []
      : shift.assignees.map((a) => ({
          name: a.name,
          // An allocation predating the role column has none; it is one of the
          // uncapped Role's, which is what the server backfills it to.
          role: a.role ?? SERVICE_VOLUNTEER_ROLE,
          custom: !a.volunteerId,
          group: a.group || null,
          volunteerId: a.volunteerId || null,
        })),
  };
}

// fetchCurrentAdmin returns the logged-in admin's email, or null if there is no
// active admin session.
export async function fetchCurrentAdmin(): Promise<string | null> {
  const res = await fetch("/auth/me");
  if (res.status === 401) return null;
  if (!res.ok) {
    throw new Error(`Failed to check login state (${res.status})`);
  }
  const data = (await res.json()) as { email: string };
  return data.email;
}

// logout clears the admin session cookie.
export async function logout(): Promise<void> {
  const res = await fetch("/auth/logout", { method: "POST" });
  if (!res.ok) {
    throw new Error(`Failed to log out (${res.status})`);
  }
}

export async function fetchRota(): Promise<RotaShift[]> {
  const today = new Date().toLocaleDateString("en-CA");
  const res = await fetch(`/api/shifts?from=${today}`);
  if (!res.ok) {
    throw new Error(`Failed to load shifts (${res.status})`);
  }
  const data = (await res.json()) as ListShiftsResponse;
  return data.shifts.map(toRotaShift);
}

interface ApiRole {
  id: string;
  name: string;
  max: number | null;
  priority: number;
  colour: string;
}

interface ListRolesResponse {
  roles: ApiRole[];
}

// An unrecognised colour falls back to the default rather than being passed
// through — a token this build has no rule for would set a chip's colour to
// nothing at all, where the default at least renders. That can only happen
// against a newer server, which is exactly when falling back quietly is worth
// more than being precise. The Role itself survives: dropping it would hide a
// Role from the settings screen, and a Role nobody can see is one nobody can
// fix.
function toConfiguredRole(role: ApiRole): ConfiguredRole {
  return {
    id: role.id,
    name: role.name,
    max: role.max,
    priority: role.priority,
    colour: (ROLE_COLOURS as readonly string[]).includes(role.colour)
      ? (role.colour as RoleColour)
      : DEFAULT_ROLE_COLOUR,
  };
}

// fetchRoles returns the Roles the drop-in offers, highest priority first. Public, like
// the rota: the chips it colours are on a page nobody has to log in to see.
export async function fetchRoles(): Promise<ConfiguredRole[]> {
  const res = await fetch("/api/roles");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to load roles"));
  }
  const data = (await res.json()) as ListRolesResponse;
  return data.roles.map(toConfiguredRole);
}

// createRole adds a Role. Admin-only, and the server mints the id: it is what
// every later reference is written against, so nothing outside the server
// chooses it.
//
// Resolves with nothing. The created Role comes back, but the caller re-reads
// the listing rather than splicing it in — the order Roles come back in is the
// order their seats are filled, which is the server's to decide.
export async function createRole(role: RoleEdit): Promise<void> {
  const res = await fetch("/api/roles", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(role),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to create the role"));
  }
}

// updateRole rewrites one Role, addressed by the id it was created with. Every
// editable field goes at once, so a null max says "no ceiling" rather than
// "leave the ceiling alone".
//
// There is no deleteRole, and there will not be one: a Role is permanent so
// that nothing referencing it can dangle (ADR 0006).
export async function updateRole(id: string, role: RoleEdit): Promise<void> {
  const res = await fetch(`/api/roles/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(role),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to save the role"));
  }
}

// fetchRotaDefaults reads the settings record. Admin-only: nothing a logged-out
// visitor sees needs it, unlike the Roles beside it on the same screen.
export async function fetchRotaDefaults(): Promise<RotaDefaults> {
  const res = await fetch("/api/rota-defaults");
  if (!res.ok) {
    throw new Error(
      await errorMessage(res, "Failed to load the rota defaults"),
    );
  }
  return (await res.json()) as RotaDefaults;
}

// saveShiftTimeDefaults writes the default shift start, end and timezone. All
// three go at once because they are one form and one idea: a time of day means
// nothing without the zone it is read in.
//
// Each section of the settings has its own endpoint, and each resolves with the
// whole record rather than with nothing — partly because the server fills in a
// timezone an admin left blank, and partly so a caller holds one thing after
// saving any section.
export async function saveShiftTimeDefaults(
  times: ShiftTimes,
): Promise<RotaDefaults> {
  const res = await fetch("/api/rota-defaults/shift-times", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(times),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to save the shift times"));
  }
  return (await res.json()) as RotaDefaults;
}

// saveDefaultShape writes what every shift asks for, stated whole: a Role
// missing from `seats` is a Role the Shape no longer asks for, which is the only
// way to say it.
export async function saveDefaultShape(
  seats: { roleId: string; count: number }[],
): Promise<RotaDefaults> {
  const res = await fetch("/api/rota-defaults/shape", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seats }),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to save the shape"));
  }
  return (await res.json()) as RotaDefaults;
}

// saveAllocationSettings writes which optional allocator rules apply. Its own
// endpoint, because the settings screen is sections and saving one must not
// blank another.
//
// Resolves with the section as the server now holds it, which is not always
// what was sent: an answer naming a rule this server does not have is dropped.
export async function saveAllocationSettings(
  settings: AllocationSettings,
): Promise<AllocationSettings> {
  const res = await fetch("/api/rota-defaults/allocation-settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
  if (!res.ok) {
    throw new Error(
      await errorMessage(res, "Failed to save the allocation rules"),
    );
  }
  return (await res.json()) as AllocationSettings;
}

interface ApiPreallocation {
  id: string;
  date: string;
  role: string;
  volunteerId?: string;
  custom?: string;
  name: string;
}

interface ListPreallocationsResponse {
  preallocations: ApiPreallocation[];
}

function toPreallocation(p: ApiPreallocation): Preallocation {
  return {
    id: p.id,
    date: p.date,
    role: p.role,
    name: p.name,
    custom: !p.volunteerId,
    volunteerId: p.volunteerId ?? null,
  };
}

// fetchPreallocations returns everyone already pinned to a shift from today
// onwards, ordered by date. Admin-only: a pin names someone against a date the
// rota has not published.
export async function fetchPreallocations(): Promise<Preallocation[]> {
  const today = new Date().toLocaleDateString("en-CA");
  const res = await fetch(`/api/preallocations?from=${today}`);
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to load preallocations"));
  }
  const data = (await res.json()) as ListPreallocationsResponse;
  return data.preallocations.map(toPreallocation);
}

// createPreallocation pins one person to a shift ahead of allocation, so the
// allocator has to place them there. Admin-only, and refused once the rota has
// been allocated — a pin can only promise something that has not happened yet.
//
// Resolves with nothing: the created pin comes back, but a caller showing pins
// is showing them sorted server-side, so it re-reads the listing rather than
// splicing this one in. Throws the server's own message,
// which names what it clashed with ("every Team lead seat for … is already
// pinned").
export async function createPreallocation(
  pin: NewPreallocation,
): Promise<void> {
  // Every pin names the role it fills — a pin is a promise about a job, and
  // the API refuses one the pinned volunteer does not hold.
  const body: Record<string, string> = {
    date: pin.date,
    role: pin.role,
  };
  if ("volunteerId" in pin.person) {
    body.volunteerId = pin.person.volunteerId;
  } else {
    body.custom = pin.person.custom;
  }

  const res = await fetch("/api/preallocations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to pin someone"));
  }
}

// deletePreallocation removes one pin by id. Any of them can go: there is one
// kind of pin, and an admin may take back any promise the rota has not been
// allocated on.
export async function deletePreallocation(id: string): Promise<void> {
  const res = await fetch(`/api/preallocations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to remove the pin"));
  }
}

interface ApiStandingPreallocation {
  id: string;
  rrule: string;
  roleId: string;
  role: string;
  volunteerId?: string;
  custom?: string;
  name: string;
}

interface ListStandingPreallocationsResponse {
  standingPreallocations: ApiStandingPreallocation[];
}

// fetchStandingPreallocations returns the pins an admin has said to make every
// rota, in the order the settings screen lists them. Admin-only.
export async function fetchStandingPreallocations(): Promise<
  StandingPreallocation[]
> {
  const res = await fetch("/api/standing-preallocations");
  if (!res.ok) {
    throw new Error(
      await errorMessage(res, "Failed to load the standing preallocations"),
    );
  }
  const data = (await res.json()) as ListStandingPreallocationsResponse;
  return data.standingPreallocations.map((s) => ({
    id: s.id,
    rrule: s.rrule,
    roleId: s.roleId,
    role: s.role,
    name: s.name,
    custom: !s.volunteerId,
    volunteerId: s.volunteerId ?? null,
  }));
}

// createStandingPreallocation adds one. Throws the server's own message, which
// names what was wrong with the rule or who was already promised those shifts.
export async function createStandingPreallocation(
  standing: NewStandingPreallocation,
): Promise<void> {
  const body: Record<string, string> = {
    rrule: standing.rrule,
    roleId: standing.roleId,
  };
  if ("volunteerId" in standing.person) {
    body.volunteerId = standing.person.volunteerId;
  } else {
    body.custom = standing.person.custom;
  }

  const res = await fetch("/api/standing-preallocations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to add the pin"));
  }
}

// deleteStandingPreallocation removes one. The pins it has already seeded belong
// to the rotas that minted them and are left exactly as they are.
export async function deleteStandingPreallocation(id: string): Promise<void> {
  const res = await fetch(
    `/api/standing-preallocations/${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to remove the pin"));
  }
}

// fetchVolunteers returns the whole synced roster, inactive volunteers included,
// already sorted by name server-side. Admin-only.
export async function fetchVolunteers(): Promise<Volunteer[]> {
  const res = await fetch("/api/volunteers");
  if (!res.ok) {
    throw new Error(`Failed to load volunteers (${res.status})`);
  }
  const data = (await res.json()) as ListVolunteersResponse;
  return data.volunteers.map(toVolunteer);
}

// defineRota defines the next rota — the weeks after the latest existing one —
// and returns the shifts it minted. Admin-only, and deliberately not idempotent:
// two calls define two consecutive rotas, so the caller is expected to show what
// came back rather than treat it as a repeatable action.
export async function defineRota(shiftCount: number): Promise<DefinedRota> {
  const res = await fetch("/api/rotations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ shiftCount }),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to define the rota"));
  }
  const data = (await res.json()) as DefineRotaResponse;
  return {
    id: data.rotation.id,
    start: data.rotation.start,
    end: data.rotation.end,
    shiftDates: data.shifts.map((s) => s.date),
  };
}

// The endpoint answers with a nullable rotation rather than a 404, because "no
// rota is in flight" is an answer: it is the state a rota may be defined in.
interface RotaInFlightResponse {
  rotation: RotaInFlight | null;
}

// fetchRotaInFlight reads the rota being worked on, or null when there is none —
// which is also the answer to "may I define one". Admin-only.
export async function fetchRotaInFlight(): Promise<RotaInFlight | null> {
  const res = await fetch("/api/rotations/in-flight");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to load the rota"));
  }
  const data = (await res.json()) as RotaInFlightResponse;
  return data.rotation;
}

// discardRota destroys an unallocated rota and everything hanging off it: its
// shifts, what each asks for, every pin on them, and the whole availability
// round including answers already given. Nothing is recoverable, and the server
// refuses outright for a rota that has been allocated.
//
// It takes no confirmation of its own. The decision is made in front of the
// numbers on the screen, and a token echoed back here would make the guarantee
// depend on the caller rather than on the server.
export async function discardRota(id: string): Promise<void> {
  const res = await fetch(`/api/rotations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to discard the rota"));
  }
}

// A person becomes either an id field (`in`/`out`) or a custom-entry field
// (`inCustom`/`outCustom`), never both — the API distinguishes the two.
function personFields(
  direction: "in" | "out",
  person: PersonRef,
): Record<string, string> {
  return "volunteerId" in person
    ? { [direction]: person.volunteerId }
    : { [`${direction}Custom`]: person.custom };
}

// createAlteration records one change to a published rota: an add, a remove, a
// move or a swap (see RotaChange). It resolves on success and throws the
// server's own message otherwise — a 409 explains which volunteer contradicts
// the shift's current state, which is worth showing the admin verbatim.
//
// The rota it returns is not the changed one: alterations are layered over
// allocations server-side, so the caller re-fetches the shifts rather than
// patching what it has.
export async function createAlteration(change: RotaChange): Promise<void> {
  const body: Record<string, string> = {
    date: change.date,
    reason: change.reason,
  };
  if (change.in) Object.assign(body, personFields("in", change.in));
  if (change.out) Object.assign(body, personFields("out", change.out));
  if (change.swapDate) body.swapDate = change.swapDate;
  if (change.role) body.role = change.role;

  const res = await fetch("/api/alterations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to change the rota"));
  }
}

// patchShift is the one write behind every per-shift edit. Each field is
// optional and an omitted one is left alone, so the two wrappers below can send
// only what they change.
//
// None of them return anything: the caller re-reads the rota, since a shift's
// state changes what else is shown against it.
async function patchShift(
  shiftId: string,
  body: { closed?: boolean; start?: string; end?: string },
  fallback: string,
): Promise<void> {
  const res = await fetch(`/api/shifts/${encodeURIComponent(shiftId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, fallback));
  }
}

// setShiftClosed closes or reopens one shift, which is a change to what
// allocation will do rather than to a rota that has been run. It throws the
// server's own message on a refusal — a 409 says the rota has already been
// allocated, which is exactly what the admin needs to read.
export function setShiftClosed(
  shiftId: string,
  closed: boolean,
): Promise<void> {
  return patchShift(
    shiftId,
    { closed },
    closed ? "Failed to close the shift" : "Failed to reopen the shift",
  );
}

// setShiftTimes moves one shift's start and end. Unlike closing it, this is
// allowed after the rota has been allocated: the times are descriptive, and the
// rota was not solved around them. A 409 means the new start lands on a day
// another shift already holds, and says which day.
//
// The times are local wall-clock, spelled as the listing spells them and as a
// datetime-local field carries them — never an instant.
export function setShiftTimes(
  shiftId: string,
  start: string,
  end: string,
): Promise<void> {
  return patchShift(shiftId, { start, end }, "Failed to save the shift times");
}

// setShiftShape rewrites what one shift asks for, stated whole for the same
// reason the default Shape is: a Role missing from `seats` is a Role that shift
// no longer asks for.
//
// Its own endpoint rather than another field of the PATCH above, because those
// two absences would mean opposite things in one body — an omitted field is
// left alone, an omitted Role is dropped. A 409 means the rota has been
// allocated, so its Shapes are what it was made from, or somebody is pinned to
// a Seat the new Shape does not offer; either way the message says which.
export async function setShiftShape(
  shiftId: string,
  seats: { roleId: string; count: number }[],
): Promise<void> {
  const res = await fetch(`/api/shifts/${encodeURIComponent(shiftId)}/shape`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seats }),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to save the shift shape"));
  }
}

interface ApiAvailabilityEntry {
  volunteerId: string;
  volunteerName: string;
  link: string;
  sentAt?: string;
  replied: boolean;
  submittedAt?: string;
  availableShiftIds: string[] | null;
  coveredBy?: string[];
  roles: string[] | null;
}

interface ApiAvailabilityRound {
  rotaId: string;
  start: string;
  end: string;
  allocated: boolean;
  shifts: {
    id: string;
    date: string;
    closed: boolean;
    needed: number;
    pinned: number;
    available: number;
    delta: number;
    roles: {
      role: string;
      capped: boolean;
      seats: number;
      pinned: number;
      needed: number;
      available: number;
      delta: number;
    }[];
  }[];
  groups: {
    key: string;
    name: string;
    replied: boolean;
    availableShiftIds: string[] | null;
    members: ApiAvailabilityEntry[] | null;
  }[];
}

interface ApiAvailabilityForm {
  volunteerName: string;
  groupMembers: string[] | null;
  shifts: {
    id: string;
    date: string;
    start: string;
    end: string;
    closed: boolean;
  }[];
  selectedShiftIds: string[] | null;
  submitted: boolean;
  submittedAt?: string;
}

function toEntry(e: ApiAvailabilityEntry): AvailabilityEntry {
  return {
    volunteerId: e.volunteerId,
    volunteerName: e.volunteerName,
    link: e.link,
    sentAt: e.sentAt ?? null,
    replied: e.replied,
    submittedAt: e.submittedAt ?? null,
    availableShiftIds: e.availableShiftIds ?? [],
    coveredBy: e.coveredBy ?? [],
    roles: e.roles ?? [],
  };
}

function toRound(data: ApiAvailabilityRound): AvailabilityRound {
  return {
    rotaId: data.rotaId,
    start: data.start,
    end: data.end,
    allocated: data.allocated,
    shifts: data.shifts,
    groups: data.groups.map((g) => ({
      key: g.key,
      name: g.name,
      replied: g.replied,
      availableShiftIds: g.availableShiftIds ?? [],
      members: (g.members ?? []).map(toEntry),
    })),
  };
}

function toForm(data: ApiAvailabilityForm): AvailabilityFormState {
  return {
    volunteerName: data.volunteerName,
    groupMembers: data.groupMembers ?? [],
    shifts: data.shifts,
    selectedShiftIds: data.selectedShiftIds ?? [],
    submitted: data.submitted,
    submittedAt: data.submittedAt ?? null,
  };
}

// AvailabilityLinkError is a link that will never work again, as opposed to a
// request that happened to fail. The two need different words on screen — one
// asks the volunteer to check the link, the other tells them the rota is out —
// so the reason is carried on the error rather than flattened into a message.
export class AvailabilityLinkError extends Error {
  readonly reason: AvailabilityLinkFailure;

  constructor(reason: AvailabilityLinkFailure) {
    super(reason);
    this.name = "AvailabilityLinkError";
    this.reason = reason;
  }
}

function linkFailure(status: number): AvailabilityLinkError | null {
  if (status === 404) return new AvailabilityLinkError("not-found");
  if (status === 410) return new AvailabilityLinkError("gone");
  return null;
}

// fetchAvailabilityForm loads what is behind a volunteer's link. Public: the
// link is the identity, and volunteers never log in.
//
// The token appears in two URLs: /availability/{token} is the page the volunteer
// is emailed, this is the payload behind it.
export async function fetchAvailabilityForm(
  token: string,
): Promise<AvailabilityFormState> {
  const res = await fetch(`/api/availability/${encodeURIComponent(token)}`);
  if (!res.ok) {
    throw (
      linkFailure(res.status) ??
      new Error(
        await errorMessage(res, "Failed to load your availability form"),
      )
    );
  }
  return toForm((await res.json()) as ApiAvailabilityForm);
}

// submitAvailability records one complete answer. shiftIds is everything the
// volunteer is available for, never just what changed: an absent shift is a no,
// so a partial send would record unavailability they did not give.
export async function submitAvailability(
  token: string,
  shiftIds: string[],
): Promise<AvailabilityFormState> {
  const res = await fetch(`/api/availability/${encodeURIComponent(token)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ shiftIds }),
  });
  if (!res.ok) {
    throw (
      linkFailure(res.status) ??
      new Error(await errorMessage(res, "Failed to send your availability"))
    );
  }
  return toForm((await res.json()) as ApiAvailabilityForm);
}

// fetchAvailabilityRound reads the latest rota's round: who was asked, their
// link, and who has answered. Admin-only — it returns every volunteer's link.
export async function fetchAvailabilityRound(): Promise<AvailabilityRound> {
  const res = await fetch("/api/availability-rounds");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to load the round"));
  }
  return toRound((await res.json()) as ApiAvailabilityRound);
}

// mintAvailabilityRound creates a link for every active volunteer on the latest
// rota. Safe to repeat: running it again after the roster changes tops the round
// up without replacing links already handed out.
export async function mintAvailabilityRound(): Promise<AvailabilityRound> {
  const res = await fetch("/api/availability-rounds", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to start the round"));
  }
  return toRound((await res.json()) as ApiAvailabilityRound);
}

interface ApiSendOutcome {
  volunteerId: string;
  volunteerName: string;
  email?: string;
  error?: string;
}

interface ApiAvailabilitySend {
  id: string;
  mode: string;
  done: number;
  total: number;
  finished: boolean;
  sent: ApiSendOutcome[] | null;
  failed: ApiSendOutcome[] | null;
  error?: string;
}

function toOutcome(o: ApiSendOutcome): SendOutcome {
  return {
    volunteerId: o.volunteerId,
    volunteerName: o.volunteerName,
    email: o.email ?? null,
    error: o.error ?? null,
  };
}

// sendUrl is the address that starts a send. Navigating to it — not fetching it
// — is the point: the server answers with a redirect to Google for the
// gmail.send scope, and only a real navigation can carry the admin through a
// consent screen and back.
//
// The deadline is quoted in the email and nowhere else. It is not stored, not
// shown on the site and not enforced; allocation is the real cutoff.
export function sendUrl(
  mode: SendMode,
  deadline: string,
  volunteerId?: string,
): string {
  const params = new URLSearchParams({ mode, deadline });
  if (volunteerId) params.set("volunteerId", volunteerId);
  return `/auth/gmail?${params.toString()}`;
}

// fetchSend reports on a send in progress or just finished. Admin-only, and
// readable only by the admin who started it: it names every volunteer it reached
// and every address it failed on.
//
// A send that has aged out of the server's memory is a 404, which is also the
// answer for one that never existed — the same thing to a page that has an id
// from an old tab.
export async function fetchSend(id: string): Promise<AvailabilitySend> {
  const res = await fetch(`/api/availability-sends/${encodeURIComponent(id)}`);
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to read the send"));
  }
  const data = (await res.json()) as ApiAvailabilitySend;
  return {
    id: data.id,
    mode: data.mode as SendMode,
    done: data.done,
    total: data.total,
    finished: data.finished,
    sent: (data.sent ?? []).map(toOutcome),
    failed: (data.failed ?? []).map(toOutcome),
    error: data.error ?? null,
  };
}

// syncVolunteers re-reads the roster sheet into the database. The server uses
// its own service account, so this is a plain authenticated POST with no OAuth
// redirect dance.
export async function syncVolunteers(): Promise<void> {
  const res = await fetch("/auth/sync", { method: "POST" });
  if (!res.ok) {
    throw new Error(`Failed to sync volunteers (${res.status})`);
  }
}
