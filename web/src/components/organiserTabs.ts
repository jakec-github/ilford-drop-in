import type { ComponentType } from "react";
import OrganiserAllocation from "./OrganiserAllocation";
import OrganiserSettings from "./OrganiserSettings";
import OrganiserVolunteers from "./OrganiserVolunteers";

// A tab is one Organiser route. Tabs without a Panel are stubs: the route and the
// tab exist so the shape of the Organiser area is visible, but the tool behind it
// is not built yet.
export interface OrganiserTab {
  path: string;
  label: string;
  Panel?: ComponentType;
  // Widens the Organiser shell for a tool whose content is a grid rather than a
  // column. Opt-in per tab rather than applied to the whole Organiser area: a list
  // of volunteers reads worse stretched across a desktop, a matrix of dates
  // reads better.
  wide?: boolean;
}

// The tab list drives both the routes (in App) and the tab bar (in OrganiserPage),
// so a new Organiser tool is one entry here plus its panel component.
//
// There is no Rota tab and no Availability tab. Both were about the rota in
// flight, and neither could be finished before the other started — the shifts
// are edited while the answers come in — so they are one Allocation tab now
// (issue #145). An allocated rota has no tab at all: it is the rota, and the
// rota page is what shows one.
export const ORGANISER_TABS: OrganiserTab[] = [
  {
    path: "/organiser/volunteers",
    label: "Volunteers",
    Panel: OrganiserVolunteers,
  },
  { path: "/organiser/settings", label: "Settings", Panel: OrganiserSettings },
  {
    path: "/organiser/allocation",
    label: "Allocation",
    Panel: OrganiserAllocation,
    wide: true,
  },
];
