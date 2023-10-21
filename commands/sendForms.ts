import { getRota } from '../services/getRota.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { bulkEmailForm } from '../services/bulkEmailForm.js';
import type { FormWithVolunteer } from '../services/bulkEmailForm.js';
import { ServiceVolunteer } from '../types.js';

export const sendForms = async (deadline: string) => {
  const volunteers = await listVolunteers();

  const rota = await getRota();

  // Use previous rota to get first shift of the next rota
  const lastShift = rota[rota.length - 1][0];
  const nextShift = new Date(lastShift);
  nextShift.setDate(nextShift.getDate() + 7);
  const dateString = nextShift.toISOString().slice(0, 10);

  const forms = await getFormSheet(dateString);

  const enrichedForms: FormWithVolunteer[] = forms.map(
    ({ volunteerID, formURL }) => ({
      volunteer: mustGetVolunteerByID(volunteerID, volunteers),
      formURL,
    }),
  );

  await bulkEmailForm(enrichedForms, deadline);
};

const mustGetVolunteerByID = (id: string, volunteers: ServiceVolunteer[]) => {
  const matches = volunteers.filter((volunteer) => volunteer.id == id);

  if (matches.length === 0) {
    throw new Error(`No volunteer with ID: ${id}`);
  }

  if (matches.length > 1) {
    throw new Error(`More than one volunteer found with ID: ${id}`);
  }

  return matches[0];
};
