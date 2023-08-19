import { getSheetsClient } from '../client.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { parseVolunteers } from '../utils/parseVolunteers.js';

export const listVolunteers = async () => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const apiResponse = await client.spreadsheets.values.get({
    spreadsheetId: confidentialData.spreadsheetID,
    range: confidentialData.serviceVolunteersTab,
  });

  const spreadsheetData = apiResponse.data?.values || [];
  if (!validateSpreadsheetData(spreadsheetData)) {
    throw new Error('Spreadsheet data format unexpected');
  }
  const serviceVolunteers = parseVolunteers(spreadsheetData);

  console.table(serviceVolunteers);
  return serviceVolunteers;
};

const validateSpreadsheetData = (data: unknown): data is string[][] =>
  Array.isArray(data) &&
  data.every(
    (subArray) =>
      Array.isArray(subArray) &&
      subArray.every((element) => typeof element === 'string'),
  );
