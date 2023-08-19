import { getServiceAccount } from './utils/getServiceAccount.js';

import { google } from 'googleapis';

const ACCESS_SCOPES = [
  'https://www.googleapis.com/auth/spreadsheets', // Sheet read access
  'https://www.googleapis.com/auth/forms.body', // Form create access
  'https://www.googleapis.com/auth/forms.responses.readonly', // Form response read access
];

// TODO: Create client on start up and pass through runner
export const getJWT = async () => {
  const serviceAccount = getServiceAccount();

  const JWT = new google.auth.JWT(
    serviceAccount.client_email,
    undefined,
    serviceAccount.private_key,
    ACCESS_SCOPES,
    undefined,
    serviceAccount.private_key_id,
  );

  await JWT.authorize();

  return JWT;
};

export const getSheetsClient = async () => {
  const JWT = await getJWT();

  return google.sheets({
    version: 'v4',
    auth: JWT,
  });
};

export const getFormsClient = async () => {
  const JWT = await getJWT();

  return google.forms({
    version: 'v1',
    auth: JWT,
  });
};
