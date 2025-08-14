import dayjs, { Dayjs } from 'dayjs';

import { ASSIGNMENT_PERIOD, FEMALE, MALE, SHIFT_SIZE } from '../const.js';
import { createOriginalRota } from '../services/createOriginalRota.js';
import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { getRota } from '../services/getRota.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { GroupResponse, Shift } from '../types.js';
import { getConfig } from '../utils/getConfig.js';
import { getName } from '../utils/parseVolunteers.js';
import {
  getIndividualResponses,
  groupResponses,
  sortGroupedResponses,
} from '../utils/responses.js';
import { friendlyDay, isoDay, getNextShifts } from '../utils/shifts.js';
import { shiftMatchesRRule } from '../utils/shiftMatchesRRule.js';

type ShiftMap = Record<string, Shift>;

export const generateRota = async (
  firstShift: string,
  shiftCount: number,
  seed = 0,
) => {
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
  const rota = await getRota();

  const responses = getIndividualResponses(
    formResponses,
    formSheet,
    volunteers,
  );
  const groupedResponses = groupResponses(responses);
  const sortedGroups = sortGroupedResponses(groupedResponses, shiftCount);

  const shifts = initShifts(shiftDays, sortedGroups);
  const lastShiftNames = rota[rota.length - 1][1].volunteers;
  assignGroups(shifts, sortedGroups, shiftDays, lastShiftNames, seed);

  const shiftArray = Object.values(shifts).sort(({ date: a }, { date: b }) =>
    dayjs(a).format('YYYY-MM-DD') > dayjs(b).format('YYYY-MM-DD') ? 1 : -1,
  );

  createOriginalRota(shiftDays, shiftArray);
};

const initShifts = (
  shiftDays: Dayjs[],
  sortedGroups: GroupResponse[],
): ShiftMap => {
  const shifts: ShiftMap = shiftDays.reduce(
    (acc, day) => ({
      [isoDay(day)]: {
        date: friendlyDay(day),
        remainingAvailabilty: 0,
        teamLead: null,
        volunteerNames: fillOccupiedSlots(day, shiftDays),
        assignedMaleCount: 0,
      },
      ...acc,
    }),
    {},
  );

  sortedGroups.forEach(({ availability, volunteers }) => {
    availability.days.forEach((day) => {
      shifts[isoDay(day)].remainingAvailabilty += volunteers.length;
    });
  });

  return shifts;
};

const fillOccupiedSlots = (shift: Dayjs, shifts: Dayjs[]) => {
  const { occupiedSlots } = getConfig();
  const occupiedVolunteers: string[] = [];
  console.log(occupiedSlots);
  occupiedSlots.forEach(({ rule, volunteers }) => {
    if (shiftMatchesRRule(rule, shift, shifts[0], shifts[shifts.length - 1])) {
      occupiedVolunteers.push(...volunteers);
    }
  });
  return occupiedVolunteers;
};

// TODO: Account for over subscription (Should probably check total volunteer count/availability first)
// TODO: Sort shifts without adjacent availables higher
// TODO: Better handle team lead availabilities (right now it just looks at overall availability on the shift)
// BUG: If team lead spot is left null it will over fill volunteers
const assignGroups = (
  shifts: ShiftMap,
  groups: GroupResponse[],
  shiftDays: Dayjs[],
  lastShiftNames: string[],
  seed: number,
) => {
  if (shiftDays.length % ASSIGNMENT_PERIOD !== 0) {
    throw new Error(
      `Shift count (${shiftDays.length}) must be a multiple of the assignment period (${ASSIGNMENT_PERIOD})`,
    );
  }

  groups.forEach(({ teamLead, volunteers, availability }) => {
    const allAvailableDays = availability.responded
      ? availability.days
      : shiftDays;
    const allAvailableShifts = allAvailableDays.map(
      (day) => shifts[isoDay(day)],
    );

    let hasMinimumAvailability = true;

    if (allAvailableDays.length < shiftDays.length / ASSIGNMENT_PERIOD) {
      console.log(
        `Group with volunteer(s): ${volunteers.map((v) =>
          getName(v),
        )} does not have the minimum availability`,
      );
      console.log(`Availability is: ${allAvailableDays.length}`);
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

    let shiftsToAssignCount = shiftDays.length / ASSIGNMENT_PERIOD;

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
    shifts[currentShiftDate.add(-1, 'week').format('YYYY-MM-DD')];
  const nextShift =
    shifts[currentShiftDate.add(1, 'week').format('YYYY-MM-DD')];

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
