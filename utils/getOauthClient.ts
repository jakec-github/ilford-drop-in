import { readFileSync } from 'node:fs';

//TODO: Validation and typing
export const getOauthClient = (): any =>
  JSON.parse(readFileSync('./secrets/oauthClient.json', 'utf8'));
