export interface ConfidentialData {
  spreadsheetID: string;
  serviceVolunteersTab: string;
}

export interface ServiceVolunteer {
  firstName: string;
  lastName: string;
  role: string; // TODO: Make enum values enums
  status: string;
  gender: string;
  email: string;
  groupKey: string | null;
}