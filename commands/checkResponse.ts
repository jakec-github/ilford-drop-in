import dayjs from 'dayjs';

import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { getNextShifts } from '../utils/getNextShifts.js';

export const checkResponse = async (
  firstShift: string,
  shiftCount: number,
  volunteerID: string,
) => {
  const firstShiftDate = dayjs(firstShift);
  const previousShiftDate = firstShiftDate.add(-1, 'week');
  const previousShift = previousShiftDate.format('YYYY-MM-DD');
  const [shiftDates] = getNextShifts(previousShift, shiftCount);

  const formSheet = await getFormSheet(firstShift);
  const response = await batchGetFormResponses(
    [
      formSheet.find((form) => form.volunteerID === volunteerID)
        ?.formID as string,
    ],
    shiftDates,
  );

  console.log(response);
};
