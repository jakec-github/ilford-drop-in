import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { listVolunteers } from '../services/listVolunteers.js';
import { getNextShifts } from '../utils/getNextShifts.js';

interface Response {
  firstName: string;
  lastName: string;
  responses: string[];
}

export const viewResponses = async (firstShift: string, shiftCount: number) => {
  const volunteers = await listVolunteers();
  const formSheet = await getFormSheet(firstShift);
  const formResponses = await batchGetFormResponses(
    formSheet.map((form) => form.formID),
  );

  const [unsortedLeadResponses, unsortedVolunteerResponses]: [Response[], Response[]] = formResponses.reduce<[Response[], Response[]]>((acc, formResponse, i) => {
    const {volunteerID} = formSheet[i];
    const volunteer = volunteers.find(({id}) => id === volunteerID);

    if (volunteer === undefined) {
      throw new Error(`Cannot find volunteer with id: ${volunteerID}`)
    }

    if (volunteer.role === 'Team lead') {
      return [
        [...acc[0], {
          firstName: formSheet[i].firstName,
          lastName: formSheet[i].lastName,
          responses: formResponse,
        }],
        acc[1],
      ]
    }

    return [
      acc[0],
      [...acc[1], {
        firstName: formSheet[i].firstName,
        lastName: formSheet[i].lastName,
        responses: formResponse,
      }],
    ]
  }, [[],[]]);

  const leadResponses = sortResponses(unsortedLeadResponses);
  const volunteerResponses = sortResponses(unsortedVolunteerResponses);


  const [dates] = getNextShifts('2024-06-23', shiftCount);

  console.table(responsesToTable(leadResponses, dates));
  console.table(responsesToTable(volunteerResponses, dates));
};

const sortResponses = (responses: Response[]) => [...responses].sort(({responses: a}, {responses: b}) => {
  if (a[0] === 'No responses') {
    return 1
  }
  if (b[0] === 'No responses') {
    return -1
  }
  return b.length - a.length
});

const responsesToTable = (responses: Response[], shifts: string[]) => [
  ['', ...shifts.map((shift) => shift.slice(4, -5))],
  ...responses.map((response) => [
    `${response.firstName} ${response.lastName}`,
    ...responseToTicks(response, shifts),
  ]),
];

const responseToTicks = ({ responses }: Response, shifts: string[]) =>
  responses[0] === 'No responses'
    ? '-'.repeat(shifts.length)
    : shifts.map((shift) => (responses.includes(shift) ? 'N' : 'Y'));
