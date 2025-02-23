import {
  Availability,
  AvailabilityFormData,
  Response,
  ServiceVolunteer,
} from '../types.js';

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
