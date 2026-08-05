import type { ComponentType } from "react";
import AdminAvailability from "./AdminAvailability";
import AdminRota from "./AdminRota";
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
export const ADMIN_TABS: AdminTab[] = [
  { path: "/admin/volunteers", label: "Volunteers", Panel: AdminVolunteers },
  { path: "/admin/config", label: "Config" },
  {
    path: "/admin/availability",
    label: "Availability",
    Panel: AdminAvailability,
    wide: true,
  },
  { path: "/admin/rota", label: "Rota", Panel: AdminRota },
];
