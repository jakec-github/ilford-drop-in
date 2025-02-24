import { Shift } from '../types.js';

export const shiftsToRows = (shifts: Shift[]): string[][] =>
  shifts.map((shift) => [
    shift.date,
    shift.teamLead || '',
    ...shift.volunteerNames,
  ]);
