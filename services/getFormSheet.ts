import { getSheetsClient } from '../client.js';
import { rowsToForms } from '../model/formData.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';
import { validateSpreadsheetData } from '../utils/validateSpreadsheetData.js';

const getFormSheetPrivate = async (firstShift: string) => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const sheetResponse = await client.spreadsheets.get({
    spreadsheetId: confidentialData.formSheetID,
  });

  const worksheetName = sheetResponse.data.sheets?.find((sheet) =>
    sheet.properties?.title?.startsWith(firstShift),
  )?.properties?.title;

  if (!worksheetName) {
    throw new Error(
      `Unable to find form sheet with expected starting data: ${firstShift}`,
    );
  }

  console.log('Worksheet name');
  console.log(worksheetName);

  const valuesResponse = await client.spreadsheets.values.get({
    spreadsheetId: confidentialData.formSheetID,
    range: worksheetName,
  });

  const spreadsheetData = valuesResponse.data?.values || [];
  if (!validateSpreadsheetData(spreadsheetData)) {
    throw new Error('Spreadsheet data format unexpected');
  }

  return rowsToForms(spreadsheetData);
};

export const getFormSheet = guardService(getFormSheetPrivate, 'Get form sheet');
