import { Dayjs } from 'dayjs';

export interface ConfidentialData {
  volunteerSheetID: string;
  serviceVolunteersTab: string;
  rotaSheetID: string;
  originalRotaSheetID: string;
  formSheetID: string;
  gmailUserID: string;
  gmailSender: string;
}

export interface Config {
  occupiedSlots: {
    type: 'RRULE';
    rule: string;
    volunteers: string[];
  }[];
}

export interface ServiceVolunteer {
  id: string;
  firstName: string;
  lastName: string;
  role: string; // TODO: Make enum values enums
  status: string;
  gender: string;
  email: string;
  groupKey: string | null;
}

export interface AvailabilityFormData {
  firstName: string;
  lastName: string;
  volunteerID: string;
  formID: string;
  formURL: string;
}

export interface Response {
  volunteer: ServiceVolunteer;
  availability: Availability;
}

export interface GroupResponse {
  teamLead: ServiceVolunteer | null;
  volunteers: ServiceVolunteer[];
  availability: Availability;
}

export interface Availability {
  responded: boolean;
  days: Dayjs[];
}

export interface Shift {
  date: string; // TODO: should be day: Dayjs
  remainingAvailabilty: number;
  teamLead: string | null;
  volunteerNames: string[];
  assignedMaleCount: number;
}
