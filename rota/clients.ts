import { readFileSync } from 'node:fs';

import { google } from 'googleapis';

const SHEETS_ACCESS_SCOPE = 'https://www.googleapis.com/auth/spreadsheets';
const FORMS_ACCESS_SCOPE = 'https://www.googleapis.com/auth/forms.body';
const MAIL_ACCESS_SCOPE = 'https://www.googleapis.com/auth/gmail.send';

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

export const getFormsClient = () => {
  const serviceAccount = JSON.parse(
    readFileSync('./secrets/serviceAccount.json', 'utf8'),
  );

  const client = new google.auth.JWT(
    serviceAccount.client_email,
    undefined,
    serviceAccount.private_key,
    [FORMS_ACCESS_SCOPE],
    undefined,
    serviceAccount.private_key_id,
  );
  return google.forms({
    version: 'v1',
    auth: client,
  });
};

export const getMailClient = () => {
  const serviceAccount = JSON.parse(
    readFileSync('./secrets/serviceAccount.json', 'utf8'),
  );

  const client = new google.auth.JWT(
    serviceAccount.client_email,
    undefined,
    serviceAccount.private_key,
    [MAIL_ACCESS_SCOPE],
    undefined,
    serviceAccount.private_key_id,
  );
  return google.gmail({
    version: 'v1',
    auth: client,
  });
};
