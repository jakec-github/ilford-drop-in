export interface ConfidentialData {
  volunteerSheetID: string;
  serviceVolunteersTab: string;
  rotaSheetID: string;
  formSheetID: string;
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
