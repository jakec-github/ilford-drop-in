import dayjs, { Dayjs } from 'dayjs';

import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { Response } from '../types.js';
import { friendlyDay, getNextShifts } from '../utils/shifts.js';
import { sortResponses, splitResponses } from '../utils/responses.js';

export const viewResponses = async (firstShift: string, shiftCount: number) => {
  const shiftDays = getNextShifts(
    dayjs(firstShift).add(-1, 'week'),
    shiftCount,
  );

  const volunteers = await listVolunteers();
  const formSheet = await getFormSheet(firstShift);
  const formResponses = await batchGetFormResponses(
    formSheet.map((form) => form.formID),
    shiftDays.map((shift) => friendlyDay(shift)),
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

  console.table(responsesToTable(leadResponses, shiftDays));
  console.table(responsesToTable(volunteerResponses, shiftDays));
};

const responsesToTable = (responses: Response[], shifts: Dayjs[]) => [
  ['', ...shifts.map((shift) => friendlyDay(shift).slice(4, -5))],
  ...responses.map((response) => [
    `${response.volunteer.firstName} ${response.volunteer.lastName}`,
    ...responseToTicks(response, shifts),
  ]),
];

const responseToTicks = ({ availability }: Response, shifts: Dayjs[]) =>
  availability.responded
    ? shifts.map((shift) =>
        availability.days.some((day) => day.isSame(shift, 'day')) ? 'Y' : 'N',
      )
    : '-'.repeat(shifts.length);
