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
  typeof (data as ConfidentialData)?.volunteerSheetID === 'string' &&
  typeof (data as ConfidentialData).serviceVolunteersTab === 'string' &&
  typeof (data as ConfidentialData).rotaSheetID === 'string' &&
  typeof (data as ConfidentialData).originalRotaSheetID === 'string' &&
  typeof (data as ConfidentialData).formSheetID === 'string' &&
  typeof (data as ConfidentialData).gmailUserID === 'string' &&
  typeof (data as ConfidentialData).gmailSender === 'string';
