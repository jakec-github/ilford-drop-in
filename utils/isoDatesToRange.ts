// Assumes dates are in chronological order
export const isoDatesToRange = (dates: string[]) =>
  `${dates[0].slice(0, 10)} - ${dates[dates.length - 1].slice(0, 10)}`;
