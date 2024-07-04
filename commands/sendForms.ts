import dayjs from 'dayjs';

import { getRota } from '../services/getRota.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { bulkEmailForm } from '../services/bulkEmailForm.js';
import type { FormWithVolunteer } from '../services/bulkEmailForm.js';
import type { ServiceVolunteer } from '../types.js';

export const sendForms = async (
  deadline: string,
  volunteerIDs: string[] = [],
) => {
  const volunteers = await listVolunteers();

  const rota = await getRota();

  // Use previous rota to get first shift of the next rota
  const lastShift = dayjs(rota[rota.length - 1][0]);
  const nextShift = lastShift.add(1, 'week');
  const dateString = nextShift.format('YYYY-MM-DD');

  const forms = await getFormSheet(dateString);

  const enrichedForms: FormWithVolunteer[] = forms
    .filter(({ volunteerID }) =>
      volunteerIDs.length ? volunteerIDs.includes(volunteerID) : true,
    )
    .map(({ volunteerID, formURL }) => ({
      volunteer: mustGetVolunteerByID(volunteerID, volunteers),
      formURL,
    }));

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
