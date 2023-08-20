import { getSheetsClient } from '../client.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';
import { validateSpreadsheetData } from '../utils/validateSpreadsheetData.js';

const getRotaPrivate = async () => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const sheetResponse = await client.spreadsheets.get({
    spreadsheetId: confidentialData.rotaSheetID,
  });

  console.log(sheetResponse.data?.sheets?.[0].properties?.gridProperties);

  const ranges = sheetResponse.data?.sheets
    ?.map(({ properties }) => properties?.title)
    .filter(rangeIsString);

  if (ranges === undefined || ranges.length === 0) {
    throw Error('No ranges found when getting rota');
  }

  const rangeResponse = await client.spreadsheets.values.get({
    spreadsheetId: confidentialData.rotaSheetID,
    range: getLastRange(ranges),
  });

  const spreadsheetData = rangeResponse.data?.values || [];
  if (!validateSpreadsheetData(spreadsheetData)) {
    throw new Error('Spreadsheet data format unexpected');
  }

  return parseRota(spreadsheetData);
};

export const getRota = guardService(getRotaPrivate, 'Get rota');

const getLastRange = (ranges: string[]): string => {
  let sortedRanges = [...ranges];

  const getlatestDate = (range: string): Date => new Date(range.slice(-10));
  sortedRanges.sort((a, b) => (getlatestDate(a) >= getlatestDate(b) ? 1 : -1));

  return sortedRanges[sortedRanges.length - 1];
};

const rangeIsString = (range: unknown): range is string =>
  typeof range === 'string';

interface Team {
  teamLead: string;
  volunteers: string[];
}

type Rota = [string, Team][];

const parseRota = (data: string[][]): Rota => {
  const indexes = data[0].reduce(
    (acc, field, i) => {
      switch (field) {
        case 'Date':
          acc.dates = i;
          break;
        case 'Team lead':
          acc.teamLead = i;
          break;
        case 'Volunteer':
          acc.volunteers.push(i);
        default:
          break;
      }
      return acc;
    },
    {
      dates: -1,
      teamLead: -1,
      volunteers: [] as number[],
    },
  );

  return data.slice(1).map((row) => {
    const date = row[indexes.dates];
    const team: Team = {
      teamLead: row[indexes.teamLead],
      volunteers: indexes.volunteers.map((i) => row[i]),
    };

    return [date, team];
  });
};
