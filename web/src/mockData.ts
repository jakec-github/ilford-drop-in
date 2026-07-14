import type { RotaShift } from "./types";

function sunday(weeksFromNow: number): string {
  const today = new Date();
  const daysUntilSunday = today.getDay() === 0 ? 0 : 7 - today.getDay();
  const date = new Date(today);
  date.setDate(today.getDate() + daysUntilSunday + weeksFromNow * 7);
  return date.toDateString();
}

export const mockRota: RotaShift[] = [
  {
    date: sunday(0),
    teamLead: "Sarah Johnson",
    volunteers: ["Amit Patel", "David Chen", "Fatima Hassan"],
    hotFood: "",
    collection: "",
  },
  {
    date: sunday(1),
    teamLead: "",
    volunteers: ["Raj Sharma", "Tom Baker"],
    hotFood: "",
    collection: "",
  },
  {
    date: sunday(2),
    teamLead: "Michael Osei",
    volunteers: [
      "Claire Williams",
      "Priya Kaur",
      "Amit Patel",
      "David Chen",
      "Fatima Hassan",
      "Tom Baker",
      "Raj Sharma",
    ],
    hotFood: "",
    collection: "",
  },
  {
    date: sunday(3),
    teamLead: "CLOSED",
    volunteers: [],
    hotFood: "",
    collection: "",
  },
  {
    date: sunday(4),
    teamLead: "Sarah Johnson",
    volunteers: ["Amit Patel", "Priya Kaur", "Fatima Hassan"],
    hotFood: "",
    collection: "",
  },
  {
    date: sunday(5),
    teamLead: "Michael Osei",
    volunteers: ["Tom Baker", "Claire Williams"],
    hotFood: "",
    collection: "",
  },
  {
    date: sunday(6),
    teamLead: "Sarah Johnson",
    volunteers: ["David Chen", "Raj Sharma"],
    hotFood: "",
    collection: "",
  },
];
