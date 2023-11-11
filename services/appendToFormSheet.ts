import { getSheetsClient } from '../client.js';
import { formsToRows } from '../model/formData.js';
import { AvailabilityFormData } from '../types.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';

const appendToFormSheetPrivate = async (
  worksheetTitle: string,
  forms: AvailabilityFormData[],
) => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  // Create worksheet
  await client.spreadsheets.values.append({
    // Path parameters
    spreadsheetId: confidentialData.formSheetID,
    range: worksheetTitle,
    // Query parameters
    valueInputOption: 'RAW',
    // Body
    requestBody: {
      values: formsToRows(forms),
    },
  });
};

export const appendToFormSheet = guardService(
  appendToFormSheetPrivate,
  'Append to form sheet',
);
