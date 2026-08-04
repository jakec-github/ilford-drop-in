import type {
  AvailabilityEntry,
  AvailabilityFormState,
  AvailabilityLinkFailure,
  AvailabilityRound,
  AvailabilitySend,
  DefinedRota,
  NewPreallocation,
  PersonRef,
  Preallocation,
  PreallocationSource,
  RotaChange,
  RotaShift,
  SendMode,
  SendOutcome,
  Volunteer,
} from "./types";

// The API's role names, as stored against an allocation. The frontend's Role
// is a two-value union, so these are the only two strings that cross the wire.
const TEAM_LEAD_ROLE = "Team lead";
const SERVICE_VOLUNTEER_ROLE = "Service volunteer";

interface ApiAssignee {
  volunteerId?: string;
  customEntry?: string;
  name: string;
  role?: string;
  group?: string;
}

interface ApiShift {
  date: string;
  start: string;
  end: string;
  closed: boolean;
  allocated: boolean;
  assignees: ApiAssignee[];
}

interface ListShiftsResponse {
  shifts: ApiShift[];
}

interface ApiVolunteer {
  id: string;
  name: string;
  fullName: string;
  role?: string;
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
    role: v.role === TEAM_LEAD_ROLE ? "lead" : "volunteer",
    group: v.group || null,
    gender: v.gender || null,
    active: v.active,
  };
}

function toRotaShift(shift: ApiShift): RotaShift {
  return {
    date: shift.date,
    closed: shift.closed,
    allocated: shift.allocated,
    // Closed shifts carry no meaningful assignees.
    assignees: shift.closed
      ? []
      : shift.assignees.map((a) => ({
          name: a.name,
          role: a.role === TEAM_LEAD_ROLE ? "lead" : "volunteer",
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

interface ApiPreallocation {
  id?: string;
  date: string;
  role: string;
  volunteerId?: string;
  custom?: string;
  name: string;
  source: string;
}

interface ListPreallocationsResponse {
  preallocations: ApiPreallocation[];
}

function toPreallocation(p: ApiPreallocation): Preallocation {
  return {
    id: p.id ?? null,
    date: p.date,
    role: p.role === TEAM_LEAD_ROLE ? "lead" : "volunteer",
    name: p.name,
    custom: !p.volunteerId,
    volunteerId: p.volunteerId ?? null,
    // Anything the server does not name as a config pin is treated as manual:
    // manual is the weaker claim, and a mislabelled pin must not read as one
    // this UI cannot explain how to change.
    source: (p.source === "config"
      ? "config"
      : "manual") as PreallocationSource,
  };
}

// fetchPreallocations returns everyone already pinned to a shift from today
// onwards — both the config-derived pins and the manual ones — ordered by date.
// Admin-only: a pin names someone against a date the rota has not published.
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
// is showing both sources merged and sorted server-side, so it re-reads the
// listing rather than splicing this one in. Throws the server's own message,
// which names what it clashed with ("a team lead is already pinned to …").
export async function createPreallocation(
  pin: NewPreallocation,
): Promise<void> {
  const body: Record<string, string | boolean> = { date: pin.date };
  if ("volunteerId" in pin.person) {
    body.volunteerId = pin.person.volunteerId;
    // Sent only when it is true: the API refuses teamLead for a volunteer the
    // roster does not record as one, so sending false everywhere else would be
    // an extra way to get it wrong.
    if (pin.role === "lead") body.teamLead = true;
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

// deletePreallocation removes one manual pin by id. Config pins have no id and
// no row behind them, so there is nothing here to address one at: changing one
// means editing the server's config.
export async function deletePreallocation(id: string): Promise<void> {
  const res = await fetch(`/api/preallocations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
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
  if (change.role) {
    body.role =
      change.role === "lead" ? TEAM_LEAD_ROLE : SERVICE_VOLUNTEER_ROLE;
  }

  const res = await fetch("/api/alterations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to change the rota"));
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
    hasTeamLead: boolean;
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
  shifts: { id: string; date: string; closed: boolean }[];
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
