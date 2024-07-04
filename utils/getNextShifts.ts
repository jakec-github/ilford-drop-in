import dayjs from 'dayjs';

export const getNextShifts = (lastShift: string, shiftCount: number) => {
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
