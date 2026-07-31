import type {
  AvailabilityFormState,
  AvailabilityLinkFailure,
  AvailabilityRound,
  DefinedRota,
  PersonRef,
  RotaChange,
  RotaShift,
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
  const res = await fetch(`/shifts?from=${today}`);
  if (!res.ok) {
    throw new Error(`Failed to load shifts (${res.status})`);
  }
  const data = (await res.json()) as ListShiftsResponse;
  return data.shifts.map(toRotaShift);
}

// fetchVolunteers returns the whole synced roster, inactive volunteers included,
// already sorted by name server-side. Admin-only.
export async function fetchVolunteers(): Promise<Volunteer[]> {
  const res = await fetch("/volunteers");
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
  const res = await fetch("/rotations", {
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

  const res = await fetch("/alterations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to change the rota"));
  }
}

interface ApiAvailabilityRound {
  rotaId: string;
  start: string;
  end: string;
  allocated: boolean;
  shifts: { id: string; date: string; closed: boolean }[];
  entries: {
    volunteerId: string;
    volunteerName: string;
    link: string;
    replied: boolean;
    submittedAt?: string;
    availableShiftIds: string[] | null;
    coveredBy?: string[];
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

function toRound(data: ApiAvailabilityRound): AvailabilityRound {
  return {
    rotaId: data.rotaId,
    start: data.start,
    end: data.end,
    allocated: data.allocated,
    shifts: data.shifts,
    entries: data.entries.map((e) => ({
      volunteerId: e.volunteerId,
      volunteerName: e.volunteerName,
      link: e.link,
      replied: e.replied,
      submittedAt: e.submittedAt ?? null,
      availableShiftIds: e.availableShiftIds ?? [],
      coveredBy: e.coveredBy ?? [],
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

// The volunteer's link is one URL for two audiences: a browser navigating to it
// gets this app, and this fetch gets the JSON behind it. Asking for JSON
// explicitly is what tells the two apart.
const JSON_ACCEPT = { Accept: "application/json" };

function linkFailure(status: number): AvailabilityLinkError | null {
  if (status === 404) return new AvailabilityLinkError("not-found");
  if (status === 410) return new AvailabilityLinkError("gone");
  return null;
}

// fetchAvailabilityForm loads what is behind a volunteer's link. Public: the
// link is the identity, and volunteers never log in.
export async function fetchAvailabilityForm(
  token: string,
): Promise<AvailabilityFormState> {
  const res = await fetch(`/availability/${encodeURIComponent(token)}`, {
    headers: JSON_ACCEPT,
  });
  if (!res.ok) {
    throw (
      linkFailure(res.status) ??
      new Error(await errorMessage(res, "Failed to load your availability form"))
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
  const res = await fetch(`/availability/${encodeURIComponent(token)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...JSON_ACCEPT },
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
  const res = await fetch("/availability-rounds");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to load the round"));
  }
  return toRound((await res.json()) as ApiAvailabilityRound);
}

// mintAvailabilityRound creates a link for every active volunteer on the latest
// rota. Safe to repeat: running it again after the roster changes tops the round
// up without replacing links already handed out.
export async function mintAvailabilityRound(): Promise<AvailabilityRound> {
  const res = await fetch("/availability-rounds", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "Failed to start the round"));
  }
  return toRound((await res.json()) as ApiAvailabilityRound);
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
