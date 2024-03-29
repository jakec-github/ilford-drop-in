import { getSheetsClient } from '../client.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';
import { parseVolunteers } from '../utils/parseVolunteers.js';
import { validateSpreadsheetData } from '../utils/validateSpreadsheetData.js';

const listVolunteersPrivate = async () => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const apiResponse = await client.spreadsheets.values.get({
    spreadsheetId: confidentialData.volunteerSheetID,
    range: confidentialData.serviceVolunteersTab,
  });

  const spreadsheetData = apiResponse.data?.values || [];
  if (!validateSpreadsheetData(spreadsheetData)) {
    throw new Error('Spreadsheet data format unexpected');
  }
  const serviceVolunteers = parseVolunteers(spreadsheetData);

  return serviceVolunteers;
};

export const listVolunteers = guardService(
  listVolunteersPrivate,
  'List Volunteers',
);
