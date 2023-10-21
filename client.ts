import { google } from 'googleapis';

import { getServiceAccount } from './utils/getServiceAccount.js';
import { getOauthClient } from './utils/getOauthClient.js';
import { listenForAuth } from './utils/listenForAuth.js';

const AUTH_PORT = 3000;

enum AccessScope {
  SHEETS = 'https://www.googleapis.com/auth/spreadsheets',
  FORMS_BODY = 'https://www.googleapis.com/auth/forms.body',
  FORMS_RESPONSES_READ_ONLY = 'https://www.googleapis.com/auth/forms.responses.readonly',
  MAIL_SEND = 'https://www.googleapis.com/auth/gmail.send',
}

// TODO: Create client on start up and pass through runner
export const getJWT = async (scopes: string[]) => {
  const serviceAccount = getServiceAccount();

  const JWT = new google.auth.JWT(
    serviceAccount.client_email,
    undefined,
    serviceAccount.private_key,
    scopes,
    undefined,
    serviceAccount.private_key_id,
  );

  await JWT.authorize();

  return JWT;
};

export const getSheetsClient = async () => {
  const JWT = await getJWT([AccessScope.SHEETS]);

  return google.sheets({
    version: 'v4',
    auth: JWT,
  });
};

export const getFormsClient = async () => {
  const JWT = await getJWT([
    AccessScope.FORMS_BODY,
    AccessScope.FORMS_RESPONSES_READ_ONLY,
  ]);

  return google.forms({
    version: 'v1',
    auth: JWT,
  });
};

export const getMailClient = async () => {
  const oAuth2ClientDetails = getOauthClient();

  const oAuth2Client = new google.auth.OAuth2(
    oAuth2ClientDetails.installed.client_id,
    oAuth2ClientDetails.installed.client_secret,
    `http://localhost:${AUTH_PORT}`,
  );

  const code = await listenForAuth(
    oAuth2Client,
    [AccessScope.MAIL_SEND],
    AUTH_PORT,
  );

  const { tokens } = await oAuth2Client.getToken(code);
  oAuth2Client.setCredentials(tokens);

  return google.gmail({
    version: 'v1',
    auth: oAuth2Client,
  });
};
