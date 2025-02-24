export interface ConfidentialData {
  volunteerSheetID: string;
  serviceVolunteersTab: string;
  rotaSheetID: string;
  originalRotaSheetID: string;
  formSheetID: string;
  gmailUserID: string;
  gmailSender: string;
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

export interface Availability {
  responded: boolean;
  dates: string[];
}

export interface Shift {
  date: string;
  remainingAvailabilty: number;
  teamLead: string | null;
  volunteerNames: string[];
  assignedMaleCount: number;
}
