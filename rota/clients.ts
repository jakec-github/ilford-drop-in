import { readFileSync } from 'node:fs';

import { google } from 'googleapis';

const SHEETS_ACCESS_SCOPE = 'https://www.googleapis.com/auth/spreadsheets';

export const getSheetsClient = () => {
  const serviceAccount = JSON.parse(
    readFileSync('./secrets/serviceAccount.json', 'utf8'),
  );

  const client = new google.auth.JWT(
    serviceAccount.client_email,
    undefined,
    serviceAccount.private_key,
    [SHEETS_ACCESS_SCOPE],
    undefined,
    serviceAccount.private_key_id,
  );
  return google.sheets({
    version: 'v4',
    auth: client,
  });
};
