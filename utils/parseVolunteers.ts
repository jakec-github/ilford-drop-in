import { ServiceVolunteer } from '../types';

// TODO: Remove reliability on column indexes
export const parseVolunteers = (raw: string[][]): ServiceVolunteer[] =>
  raw.slice(1).map((volunteer) => ({
    firstName: volunteer[0],
    lastName: volunteer[1],
    role: volunteer[2], // TODO: Make enum values enums
    status: volunteer[3],
    gender: volunteer[4],
    email: volunteer[5],
    groupKey: volunteer[10] || null,
  }));
