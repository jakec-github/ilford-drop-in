import { AvailabilityFormData } from '../types.js';

export const FIELDS = [
  'First name',
  'Last name',
  'Form URL',
  'Form ID',
  'Volunteer ID',
];

export const formsToRows = (forms: AvailabilityFormData[]): string[][] =>
  forms.map((form) => {
    const row = Array(FIELDS.length);

    row[getTitleIndex('First name')] = form.firstName;
    row[getTitleIndex('Last name')] = form.lastName;
    row[getTitleIndex('Form URL')] = form.formURL;
    row[getTitleIndex('Form ID')] = form.formID;
    row[getTitleIndex('Volunteer ID')] = form.volunteerID;

    return row;
  });

export const rowsToForms = (rows: string[][]): AvailabilityFormData[] => {
  const fieldIndexes = new Map<string, number>();

  FIELDS.forEach((field) => {
    fieldIndexes.set(
      field,
      rows[0].findIndex((text) => text === field),
    );
  });

  const getField = (field: string, row: string[]): string => {
    const index = fieldIndexes.get(field);

    if (index === undefined || index === -1) {
      throw Error(`Missing field in volunteers data: ${field}`);
    }

    return row[index];
  };

  return rows.slice(1).map((row) => ({
    firstName: getField('First name', row),
    lastName: getField('Last name', row),
    formURL: getField('Form URL', row),
    formID: getField('Form ID', row),
    volunteerID: getField('Volunteer ID', row),
  }));
};

const getTitleIndex = (title: string) =>
  FIELDS.findIndex((text) => text === title);
