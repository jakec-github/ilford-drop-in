import { readFileSync } from 'node:fs';

import { getSheetsClient } from './clients.js';

export interface ServiceVolunteer {
  firstName: string;
  lastName: string;
  role: string; // TODO: Make enum values enums
  status: string;
  gender: string;
  email: string;
  groupKey: string | null;
}

interface ConfidentialData {
  spreadsheetID: string;
  serviceVolunteersTab: string;
}

export const getServiceVolunteers = async () => {
  const service = getSheetsClient();

  const confidentialData = JSON.parse(
    readFileSync('./secrets/confidential.json', 'utf8'),
  );

  if (!validateConfidentialData(confidentialData)) {
    throw new Error('Confidential data format unexpected');
  }

  const result = await service.spreadsheets.values.get({
    spreadsheetId: confidentialData.spreadsheetID,
    range: confidentialData.serviceVolunteersTab,
  });

  const spreadsheetData = result.data?.values || [];

  if (!validateData(spreadsheetData)) {
    throw new Error('Spreadsheet data format unexpected');
  }

  return parseVolunteers(spreadsheetData);
};

const parseVolunteers = (raw: string[][]): ServiceVolunteer[] =>
  raw.slice(1).map((volunteer) => ({
    firstName: volunteer[0],
    lastName: volunteer[1],
    role: volunteer[2], // TODO: Make enum values enums
    status: volunteer[3],
    gender: volunteer[4],
    email: volunteer[5],
    groupKey: volunteer[10] || null,
  }));

const validateData = (data: unknown): data is string[][] =>
  Array.isArray(data) &&
  data.every(
    (subArray) =>
      Array.isArray(subArray) &&
      subArray.every((element) => typeof element === 'string'),
  );

const validateConfidentialData = (data: unknown): data is ConfidentialData =>
  typeof (data as ConfidentialData)?.spreadsheetID === 'string' &&
  typeof (data as ConfidentialData).serviceVolunteersTab === 'string';
