import { bulkCreateForms } from '../services/bulkCreateForm.js';
import { appendToFormSheet } from '../services/appendToFormSheet.js';
import { getRota } from '../services/getRota.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { getNextShiftsLegacy } from '../utils/shifts.js';
import { orderedDatesToRange } from '../utils/orderedDatesToRange.js';

export const addForms = async (shiftCount: number, volunteerIDs: string[]) => {
  const volunteers = await listVolunteers();
  const rota = await getRota();

  const whitelistedVolunteers = volunteers.filter(
    ({ id, status }) => volunteerIDs.includes(id) && status === 'Active',
  );

  if (whitelistedVolunteers.length === 0) {
    throw new Error('No matching active volunteers found');
  }

  const [dates, isoDates] = getNextShiftsLegacy(
    rota[rota.length - 1][0],
    shiftCount,
  );

  const worksheetTitle = orderedDatesToRange(isoDates);

  const forms = await bulkCreateForms(dates, whitelistedVolunteers);

  appendToFormSheet(worksheetTitle, forms);
};
