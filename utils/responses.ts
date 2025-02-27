import {
  Availability,
  AvailabilityFormData,
  GroupResponse,
  Response,
  ServiceVolunteer,
} from '../types.js';

export const groupResponses = (responses: Response[]): GroupResponse[] => {
  const groupResponses: Record<string, GroupResponse> = {};
  const individualResponses: GroupResponse[] = [];

  responses.forEach((response) => {
    const { volunteer, availability } = response;
    const isTeamLead = response.volunteer.role === 'Team lead';
    const { groupKey } = volunteer;
    if (groupKey) {
      if (groupResponses[groupKey]) {
        const existingGroup = groupResponses[groupKey];
        groupResponses[groupKey] = {
          teamLead: isTeamLead ? volunteer : existingGroup.teamLead,
          volunteers: [...existingGroup.volunteers, volunteer],
          availability: mergeAvailability(
            existingGroup.availability,
            availability,
          ),
        };
      } else {
        groupResponses[groupKey] = {
          teamLead: isTeamLead ? volunteer : null,
          volunteers: [volunteer],
          availability,
        };
      }
    } else {
      individualResponses.push({
        teamLead: isTeamLead ? volunteer : null,
        volunteers: [volunteer],
        availability,
      });
    }
  });

  return [...individualResponses, ...Object.values(groupResponses)];
};

export const sortGroupedResponses = (
  responses: GroupResponse[],
  shiftcount: number,
): GroupResponse[] =>
  [...responses].sort((a, b) => {
    if (a.teamLead !== null) {
      if (b.teamLead === null) {
        return -1;
      }
      // Fall through
    } else if (b.teamLead !== null) {
      return 1;
    }
    const aAvailability = a.availability.responded
      ? a.availability.dates.length
      : shiftcount + 1;
    const bAvailability = b.availability.responded
      ? b.availability.dates.length
      : shiftcount + 1;

    return (
      aAvailability -
      a.volunteers.length -
      (bAvailability - b.volunteers.length)
    );
  });

export const sortResponses = (responses: Response[]) =>
  [...responses].sort(({ availability: a }, { availability: b }) => {
    if (!a.responded) {
      return 1;
    }
    if (!b.responded) {
      return -1;
    }
    return a.dates.length - b.dates.length;
  });

export const getIndividualResponses = (
  formResponses: Availability[],
  formSheet: AvailabilityFormData[],
  volunteers: ServiceVolunteer[],
): Response[] =>
  formResponses.map((formResponse, i) => {
    const { volunteerID } = formSheet[i];
    const volunteer = volunteers.find(({ id }) => id === volunteerID);

    if (volunteer === undefined) {
      throw new Error(`Cannot find volunteer with id: ${volunteerID}`);
    }

    return {
      volunteer,
      availability: formResponse,
    };
  });

export const splitResponses = (
  formResponses: Availability[],
  formSheet: AvailabilityFormData[],
  volunteers: ServiceVolunteer[],
): [Response[], Response[]] =>
  formResponses.reduce<[Response[], Response[]]>(
    (acc, formResponse, i) => {
      const { volunteerID } = formSheet[i];
      const volunteer = volunteers.find(({ id }) => id === volunteerID);

      if (volunteer === undefined) {
        throw new Error(`Cannot find volunteer with id: ${volunteerID}`);
      }

      if (volunteer.role === 'Team lead') {
        return [
          [
            ...acc[0],
            {
              volunteer,
              availability: formResponse,
            },
          ],
          acc[1],
        ];
      }

      return [
        acc[0],
        [
          ...acc[1],
          {
            volunteer,
            availability: formResponse,
          },
        ],
      ];
    },
    [[], []],
  );

const mergeAvailability = (
  { responded: r1, dates: d1 }: Availability,
  { responded: r2, dates: d2 }: Availability,
): Availability => ({
  responded: r1 || r2,
  dates: r1 && r2 ? getDatesIntersection(d1, d2) : r1 ? d1 : d2, // Sorry
});

const getDatesIntersection = (d1: string[], d2: string[]): string[] =>
  d1.filter((date) => d2.includes(date));
