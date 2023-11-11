import { bulkCreateForms } from '../services/bulkCreateForm.js';
import { createFormSheet } from '../services/createFormSheet.js';
import { getRota } from '../services/getRota.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { getNextShifts } from '../utils/getNextShifts.js';

export const createForms = async (shiftCount: number) => {
  const volunteers = await listVolunteers();
  const rota = await getRota();

  const activeVolunteers = volunteers.filter(
    ({ status }) => status === 'Active',
  );

  const [dates, isoDates] = getNextShifts(rota[rota.length - 1][0], shiftCount);

  const forms = await bulkCreateForms(dates, activeVolunteers);

  createFormSheet(isoDates, forms);
};
