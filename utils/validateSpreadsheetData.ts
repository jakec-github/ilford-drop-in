export const validateSpreadsheetData = (data: unknown): data is string[][] =>
  Array.isArray(data) &&
  data.every(
    (subArray) =>
      Array.isArray(subArray) &&
      subArray.every((element) => typeof element === 'string'),
  );
