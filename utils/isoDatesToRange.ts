// Assumes dates are in chronological order
export const isoDatesToRange = (dates: string[]) =>
  `${dates[0]} - ${dates[dates.length - 1]}`;
