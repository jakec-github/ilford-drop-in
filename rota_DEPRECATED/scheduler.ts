import { ServiceVolunteer } from './services';

type Date = string;

interface ServiceVolunteerWithDates extends ServiceVolunteer {
  unavailableDates: string[];
}

interface Team {
  lead: ServiceVolunteer;
  members: ServiceVolunteer[];
}

type Schedule = Map<Date, Team>;

export const getSchedule = (
  volunteers: ServiceVolunteerWithDates[],
  dates: Date[],
): Schedule => {
  return new Map();
};
