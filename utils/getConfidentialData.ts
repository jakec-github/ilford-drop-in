import { readFileSync } from 'node:fs';

import { ConfidentialData } from '../types';

export const getConfidentialData = (): ConfidentialData => {
  const confidentialData: unknown = JSON.parse(
    readFileSync('./secrets/confidential.json', 'utf8'),
  );

  if (!validateConfidentialData(confidentialData)) {
    throw new Error('Confidential data format unexpected');
  }

  return confidentialData;
};

const validateConfidentialData = (data: unknown): data is ConfidentialData =>
  typeof (data as ConfidentialData)?.spreadsheetID === 'string' &&
  typeof (data as ConfidentialData).serviceVolunteersTab === 'string';
