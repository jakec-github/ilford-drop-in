import { ServiceVolunteer } from '../types';

const FIELDS = [
  'First name',
  'Last name',
  'Role',
  'Status',
  'Sex/Gender',
  'Email',
  'Group key',
];

export const parseVolunteers = (raw: string[][]): ServiceVolunteer[] => {
  const fieldIndexes = new Map<string, number>();

  FIELDS.forEach((field) => {
    fieldIndexes.set(
      field,
      raw[0].findIndex((text) => text === field),
    );
  });

  const getField = (field: string, row: string[]): string => {
    const index = fieldIndexes.get(field);

    if (index === undefined || index === -1) {
      throw Error(`Missing field in volunteers data: ${field}`);
    }

    return row[index];
  };

  return raw.slice(1).map((row) => ({
    firstName: getField('First name', row),
    lastName: getField('Last name', row),
    role: getField('Role', row), // TODO: Make enum values enums or unions
    status: getField('Status', row),
    gender: getField('Sex/Gender', row),
    email: getField('Email', row),
    groupKey: getField('Group key', row) || null,
  }));
};
