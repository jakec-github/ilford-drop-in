import type { RotaShift } from "./types";

const TEAM_LEAD_ROLE = "Team lead";

interface ApiAssignee {
  volunteerId?: string;
  customEntry?: string;
  name: string;
  role?: string;
}

interface ApiShift {
  date: string;
  start: string;
  end: string;
  closed: boolean;
  assignees: ApiAssignee[];
}

interface ListShiftsResponse {
  shifts: ApiShift[];
}

function toRotaShift(shift: ApiShift): RotaShift {
  const teamLead = shift.assignees.find((a) => a.role === TEAM_LEAD_ROLE);
  const volunteers = shift.assignees
    .filter((a) => a !== teamLead)
    .map((a) => a.name);

  return {
    date: shift.date,
    teamLead: shift.closed ? "CLOSED" : (teamLead?.name ?? ""),
    volunteers: shift.closed ? [] : volunteers,
    hotFood: "",
    collection: "",
  };
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
