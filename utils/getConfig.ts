import { readFileSync } from 'node:fs';

import { Config } from '../types';

export const getConfig = (): Config => {
  try {
    const config: unknown = JSON.parse(readFileSync('./config.json', 'utf8'));

    if (!validateConfig(config)) {
      throw new Error('Config format unexpected');
    }

    return config;
  } catch (err: any) {
    if (err.code === 'ENOENT') {
      return { occupiedSlots: [] };
    }
    throw err;
  }
};

const validateConfig = (value: unknown): value is Config => {
  if (
    typeof value !== 'object' ||
    value === null ||
    !Array.isArray((value as any).occupiedSlots)
  ) {
    return false;
  }

  return (value as Config).occupiedSlots.every(
    (slot) =>
      typeof slot === 'object' &&
      slot !== null &&
      slot.type === 'RRULE' &&
      typeof slot.rule === 'string' &&
      Array.isArray(slot.volunteers) &&
      slot.volunteers.every((v) => typeof v === 'string'),
  );
};
