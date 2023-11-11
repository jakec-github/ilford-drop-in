export const getNextShifts = (lastShift: string, shiftCount: number) => {
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
