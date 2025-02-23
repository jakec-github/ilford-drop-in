import dayjs from 'dayjs';

import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { Response } from '../types.js';
import { getNextShifts } from '../utils/getNextShifts.js';
import { sortResponses, splitResponses } from '../utils/responses.js';

export const viewResponses = async (firstShift: string, shiftCount: number) => {
  const firstShiftDate = dayjs(firstShift);
  const previousShiftDate = firstShiftDate.add(-1, 'week');
  const previousShift = previousShiftDate.format('YYYY-MM-DD');
  const [shiftDates] = getNextShifts(previousShift, shiftCount);

  const volunteers = await listVolunteers();
  const formSheet = await getFormSheet(firstShift);
  const formResponses = await batchGetFormResponses(
    formSheet.map((form) => form.formID),
    shiftDates,
  );

  const [unsortedLeadResponses, unsortedVolunteerResponses] = splitResponses(
    formResponses,
    formSheet,
    volunteers,
  );

  console.log(unsortedLeadResponses);

  const leadResponses = sortResponses(unsortedLeadResponses);
  const volunteerResponses = sortResponses(unsortedVolunteerResponses);

  console.log(leadResponses);

  console.table(responsesToTable(leadResponses, shiftDates));
  console.table(responsesToTable(volunteerResponses, shiftDates));
};

const responsesToTable = (responses: Response[], shifts: string[]) => [
  ['', ...shifts.map((shift) => shift.slice(4, -5))],
  ...responses.map((response) => [
    `${response.volunteer.firstName} ${response.volunteer.lastName}`,
    ...responseToTicks(response, shifts),
  ]),
];

const responseToTicks = ({ availability }: Response, shifts: string[]) =>
  availability.responded
    ? shifts.map((shift) => (availability.dates.includes(shift) ? 'Y' : 'N'))
    : '-'.repeat(shifts.length);
