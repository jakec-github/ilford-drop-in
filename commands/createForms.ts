import { bulkCreateForms } from '../services/bulkCreateForm.js';
import { createFormSheet } from '../services/createFormSheet.js';
import { getRota } from '../services/getRota.js';
import { listVolunteers } from '../services/listVolunteers.js';

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

const getNextShifts = (lastShift: string, shiftCount: number) => {
  let dates: string[] = [];
  let isoDates: string[] = [];
  for (let i = 1; i <= shiftCount; i += 1) {
    const shift = new Date(lastShift);
    shift.setDate(shift.getDate() + i * 7);

    dates.push(shift.toDateString());

    // Ensures that BST times still result in an ISO string with the correct date
    shift.setHours(shift.getHours() + 1);
    isoDates.push(shift.toISOString().slice(0, 10));
  }

  return [dates, isoDates];
};
