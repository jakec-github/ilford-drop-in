import type { ComponentType } from "react";
import AdminAllocation from "./AdminAllocation";
import AdminSettings from "./AdminSettings";
import AdminVolunteers from "./AdminVolunteers";

// A tab is one admin route. Tabs without a Panel are stubs: the route and the
// tab exist so the shape of the admin area is visible, but the tool behind it
// is not built yet.
export interface AdminTab {
  path: string;
  label: string;
  Panel?: ComponentType;
  // Widens the admin shell for a tool whose content is a grid rather than a
  // column. Opt-in per tab rather than applied to the whole admin area: a list
  // of volunteers reads worse stretched across a desktop, a matrix of dates
  // reads better.
  wide?: boolean;
}

// The tab list drives both the routes (in App) and the tab bar (in AdminPage),
// so a new admin tool is one entry here plus its panel component.
//
// There is no Rota tab and no Availability tab. Both were about the rota in
// flight, and neither could be finished before the other started — the shifts
// are edited while the answers come in — so they are one Allocation tab now
// (issue #145). An allocated rota has no tab at all: it is the rota, and the
// rota page is what shows one.
export const ADMIN_TABS: AdminTab[] = [
  { path: "/admin/volunteers", label: "Volunteers", Panel: AdminVolunteers },
  { path: "/admin/settings", label: "Settings", Panel: AdminSettings },
  {
    path: "/admin/allocation",
    label: "Allocation",
    Panel: AdminAllocation,
    wide: true,
  },
];
