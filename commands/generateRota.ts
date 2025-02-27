import dayjs from 'dayjs';

import { ASSIGNMENT_PERIOD, FEMALE, MALE, SHIFT_SIZE } from '../const.js';
import { createOriginalRota } from '../services/createOriginalRota.js';
import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { getRota } from '../services/getRota.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { GroupResponse, Shift } from '../types.js';
import { getNextShifts } from '../utils/getNextShifts.js';
import {
  getIndividualResponses,
  groupResponses,
  sortGroupedResponses,
} from '../utils/responses.js';
import { getName } from '../utils/parseVolunteers.js';

type ShiftMap = Record<string, Shift>;

export const generateRota = async (
  firstShift: string,
  shiftCount: number,
  seed = 0,
) => {
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
  const rota = await getRota();

  const responses = getIndividualResponses(
    formResponses,
    formSheet,
    volunteers,
  );
  const groupedResponses = groupResponses(responses);
  const sortedGroups = sortGroupedResponses(groupedResponses, shiftCount);

  const shifts = initShifts(shiftDates, sortedGroups);
  const lastShiftNames = rota[rota.length - 1][1].volunteers;
  assignGroups(shifts, sortedGroups, shiftDates, lastShiftNames, seed);

  const shiftArray = Object.values(shifts).sort(({ date: a }, { date: b }) =>
    dayjs(a).format('YYYY-MM-DD') > dayjs(b).format('YYYY-MM-DD') ? 1 : -1,
  );

  createOriginalRota(shiftDates, shiftArray);
};

const initShifts = (
  shiftDates: string[],
  sortedResponses: GroupResponse[],
): ShiftMap => {
  const shifts: ShiftMap = shiftDates.reduce(
    (acc, date) => ({
      [date]: {
        date,
        remainingAvailabilty: 0,
        teamLead: null,
        volunteerNames: [],
        assignedMaleCount: 0,
      },
      ...acc,
    }),
    {},
  );

  sortedResponses.forEach(({ availability, volunteers }) => {
    availability.dates.forEach((date) => {
      shifts[date].remainingAvailabilty += volunteers.length;
    });
  });

  return shifts;
};

// TODO: Account for over subscription (Should probably check total volunteer count/availability first)
// TODO: Sort shifts without adjacent availables higher
// BUG: If team lead spot is left null it will over fill volunteers
const assignGroups = (
  shifts: ShiftMap,
  groups: GroupResponse[],
  shiftDates: string[],
  lastShiftNames: string[],
  seed: number,
) => {
  if (shiftDates.length % ASSIGNMENT_PERIOD !== 0) {
    throw new Error(
      `Shift count (${shiftDates.length}) must be a multiple of the assignment period (${ASSIGNMENT_PERIOD})`,
    );
  }

  groups.forEach(({ teamLead, volunteers, availability }) => {
    const allAvailableDates = availability.responded
      ? availability.dates
      : shiftDates;
    const allAvailableShifts = allAvailableDates.map((date) => shifts[date]);

    let hasMinimumAvailability = true;

    if (allAvailableDates.length < shiftDates.length / ASSIGNMENT_PERIOD) {
      console.log(
        `Group with volunteer(s): ${volunteers.map((v) =>
          getName(v),
        )} does not have the minimum availability`,
      );
      console.log(`Availability is: ${allAvailableDates.length}`);
      hasMinimumAvailability = false;
    }

    // Goes negative on non-respondants but shouldn't matter
    if (availability.responded) {
      allAvailableShifts.forEach((shift) => {
        shift.remainingAvailabilty -= volunteers.length;
      });
    }

    const availableShifts = allAvailableShifts
      .filter((shift) => (teamLead ? !shift.teamLead : true))
      .filter(
        (shift) =>
          shift.volunteerNames.length + (shift.teamLead ? 1 : 0) <=
          SHIFT_SIZE - volunteers.length,
      )
      .filter(
        (shift) =>
          !(
            volunteers.every(({ gender }) => gender === FEMALE) &&
            shift.assignedMaleCount === 0 &&
            SHIFT_SIZE -
              (shift.volunteerNames.length + (shift.teamLead ? 1 : 0)) <=
              volunteers.length
          ),
      )
      .sort((shiftA, shiftB) => {
        if (volunteers.some(({ gender }) => gender === MALE)) {
          return shiftA.assignedMaleCount - shiftB.assignedMaleCount;
        }
        const bestAvailability =
          getAvailabilityPerSlot(shiftB) - getAvailabilityPerSlot(shiftA);
        if (bestAvailability !== 0) {
          return bestAvailability;
        }
        return seededRandomSign(hashStringToNumber(shiftA.date) + seed);
      });

    let shiftsToAssignCount = shiftDates.length / ASSIGNMENT_PERIOD;

    for (const shift of availableShifts) {
      if (shiftsToAssignCount == 0) {
        break;
      }
      if (
        volunteers.some((volunteer) =>
          isDoubleShift(shift, shifts, lastShiftNames, getName(volunteer)),
        )
      ) {
        continue;
      }

      if (teamLead) {
        shift.teamLead = getName(teamLead);
      }
      volunteers.forEach((volunteer) => {
        const volunteerName = getName(volunteer);
        if (volunteerName !== shift.teamLead) {
          shift.volunteerNames.push(volunteerName);
        }
        if (volunteer.gender === MALE) {
          shift.assignedMaleCount += 1;
        }
      });
      shiftsToAssignCount -= 1;
    }

    if (shiftsToAssignCount > 0) {
      if (hasMinimumAvailability) {
        console.log(
          `Group with volunteer(s): ${volunteers.map((v) =>
            getName(v),
          )} can be assigned ${shiftsToAssignCount} more shifts`,
        );
      }
    }
  });
};

const getAvailabilityPerSlot = (shift: Shift) =>
  shift.remainingAvailabilty /
  (SHIFT_SIZE - (shift.volunteerNames.length + (shift.teamLead ? 1 : 0)));

// BUG: This code assumes names have not changed. Would need to store last rota by ID
const isDoubleShift = (
  shift: Shift,
  shifts: ShiftMap,
  lastShiftNames: string[],
  volunteerName: string,
): boolean => {
  const currentShiftDate = dayjs(shift.date);
  const previousShift =
    shifts[currentShiftDate.add(-1, 'week').format('ddd MMM DD YYYY')];
  const nextShift =
    shifts[currentShiftDate.add(1, 'week').format('ddd MMM DD YYYY')];

  if (previousShift) {
    if (
      previousShift.volunteerNames.includes(volunteerName) ||
      previousShift.teamLead === volunteerName
    ) {
      return true;
    }
  } else {
    if (lastShiftNames.includes(volunteerName)) {
      return true;
    }
  }

  if (
    nextShift &&
    (nextShift.volunteerNames.includes(volunteerName) ||
      nextShift.teamLead === volunteerName)
  ) {
    return true;
  }

  return false;
};

const seededRandomSign = (seed: number): number =>
  (Math.sin(seed) * 10000) % 1 < 0.5 ? -1 : 1;

const hashStringToNumber = (str: string, max: number = 10000): number =>
  [...str].reduce((hash, char) => (hash * 31 + char.charCodeAt(0)) % max, 0);
