import { readFileSync } from 'node:fs';

//TODO: Validation and typing
export const getServiceAccount = (): any =>
  JSON.parse(readFileSync('./secrets/serviceAccount.json', 'utf8'));
