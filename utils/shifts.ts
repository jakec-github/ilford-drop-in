import dayjs, { Dayjs } from 'dayjs';

export const getNextShiftsLegacy = (lastShift: string, shiftCount: number) => {
  let dates: string[] = [];
  let isoDates: string[] = [];
  const lastShiftDate = dayjs(lastShift);

  for (let i = 1; i <= shiftCount; i += 1) {
    const shift = lastShiftDate.add(i, 'week');
    dates.push(shift.format('ddd MMM DD YYYY'));
    isoDates.push(shift.format('YYYY-MM-DD'));
  }

  return [dates, isoDates];
};

export const getNextShifts = (previousShift: Dayjs, shiftCount: number) =>
  Array.from({ length: shiftCount }, (_, i) =>
    previousShift.add(i + 1, 'week'),
  );

export const isoDay = (day: Dayjs) => day.format('YYYY-MM-DD');
export const friendlyDay = (day: Dayjs) => day.format('ddd MMM DD YYYY');
