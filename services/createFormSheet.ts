import { getSheetsClient } from '../client.js';
import { AvailabilityFormData } from '../types.js';
import { isoDatesToRange } from '../utils/isoDatesToRange.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';

const formTitles = [
  'First name',
  'Last name',
  'Form URL',
  'Form ID',
  'Volunteer ID',
];

const createFormSheetPrivate = async (
  dates: string[],
  forms: AvailabilityFormData[],
) => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const worksheetTitle = isoDatesToRange(dates);

  // Create worksheet
  await client.spreadsheets.batchUpdate({
    spreadsheetId: confidentialData.formSheetID,
    requestBody: {
      requests: [
        {
          addSheet: {
            properties: {
              title: worksheetTitle,
            },
          },
        },
      ],
    },
  });

  // Add data to worksheet
  const lastColumn = String.fromCharCode(64 + formTitles.length);
  const lastRow = forms.length + 1;

  await client.spreadsheets.values.update({
    spreadsheetId: confidentialData.formSheetID,
    range: `${worksheetTitle}!A1:${lastColumn}${lastRow}`,
    requestBody: {
      values: [formTitles, ...formsToRows(forms)],
    },
    valueInputOption: 'RAW',
  });
};

export const createFormSheet = guardService(
  createFormSheetPrivate,
  'Create form sheet',
);

const formsToRows = (forms: AvailabilityFormData[]): string[][] =>
  forms.map((form) => {
    const row = Array(formTitles.length);

    row[getTitleIndex('First name')] = form.firstName;
    row[getTitleIndex('Last name')] = form.lastName;
    row[getTitleIndex('Form URL')] = form.formURL;
    row[getTitleIndex('Form ID')] = form.formID;
    row[getTitleIndex('Volunteer ID')] = form.volunteerID;

    return row;
  });

const getTitleIndex = (title: string) =>
  formTitles.findIndex((text) => text === title);
