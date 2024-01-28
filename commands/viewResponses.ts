import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getRota } from '../services/getRota.js';
import { getFormSheet } from '../services/getFormSheet.js';
import { getNextShifts } from '../utils/getNextShifts.js';

interface Response {
  firstName: string;
  lastName: string;
  responses: string[];
}

export const viewResponses = async (firstShift: string, shiftCount: number) => {
  const formSheet = await getFormSheet(firstShift);
  const formResponses = await batchGetFormResponses(
    formSheet.map((form) => form.formID),
  );
  // TODO: Remove the reliance on the rota. Only used to get previous shift date
  const rota = await getRota();

  const responses: Response[] = formResponses.map((formResponse, i) => ({
    firstName: formSheet[i].firstName,
    lastName: formSheet[i].lastName,
    responses: formResponse,
  })); // That shows results by person

  const [dates] = getNextShifts(rota[rota.length - 1][0], shiftCount);

  console.table(responsesToTable(responses, dates));
};

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
