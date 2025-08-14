import { getSheetsClient } from '../client.js';
import { FIELDS, formsToRows } from '../model/formData.js';
import { AvailabilityFormData } from '../types.js';
import { orderedDatesToRange } from '../utils/orderedDatesToRange.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';

const createFormSheetPrivate = async (
  dates: string[],
  forms: AvailabilityFormData[],
) => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const worksheetTitle = orderedDatesToRange(dates);

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
  const lastColumn = String.fromCharCode(64 + FIELDS.length);
  const lastRow = forms.length + 1;

  await client.spreadsheets.values.update({
    // Path parameters
    spreadsheetId: confidentialData.formSheetID,
    range: `${worksheetTitle}!A1:${lastColumn}${lastRow}`,
    // Query parameters
    valueInputOption: 'RAW',
    // Body
    requestBody: {
      values: [FIELDS, ...formsToRows(forms)],
    },
  });
};

export const createFormSheet = guardService(
  createFormSheetPrivate,
  'Create form sheet',
);
