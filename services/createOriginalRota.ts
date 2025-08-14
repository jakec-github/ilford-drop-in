import { Dayjs } from 'dayjs';

import { getSheetsClient } from '../client.js';
import { shiftsToRows } from '../model/originalRota.js';
import { Shift } from '../types.js';
import { orderedDatesToRange } from '../utils/orderedDatesToRange.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';
import { friendlyDay } from '../utils/shifts.js';

const createOriginalRotaPrivate = async (days: Dayjs[], shifts: Shift[]) => {
  const client = await getSheetsClient();
  const confidentialData = getConfidentialData();

  const worksheetTitle = orderedDatesToRange(
    days.map((day) => friendlyDay(day)),
  );

  // Create worksheet
  await client.spreadsheets.batchUpdate({
    spreadsheetId: confidentialData.originalRotaSheetID,
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

  const maxNumberOfVolunteers = Math.max(
    ...shifts.map(({ volunteerNames }) => volunteerNames.length),
  );

  // Add data to worksheet
  const lastColumn = String.fromCharCode(
    64 + // Offset of character
      2 + // Date and team lead
      maxNumberOfVolunteers,
  );
  const lastRow = shifts.length + 1;

  await client.spreadsheets.values.update({
    spreadsheetId: confidentialData.originalRotaSheetID,
    range: `${worksheetTitle}!A1:${lastColumn}${lastRow}`,
    valueInputOption: 'RAW',
    requestBody: {
      values: [
        [
          'Date',
          'Team lead',
          ...Array(maxNumberOfVolunteers).fill('Volunteer'),
        ],
        ...shiftsToRows(shifts),
      ],
    },
  });
};

export const createOriginalRota = guardService(
  createOriginalRotaPrivate,
  'Create original rota',
  false,
);
