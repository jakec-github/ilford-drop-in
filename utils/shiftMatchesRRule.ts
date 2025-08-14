import dayjs, { Dayjs } from 'dayjs';
import rrule from 'rrule';

// TODO: Clear error on invalid rruleString
export const shiftMatchesRRule = (
  rruleString: string,
  shift: Dayjs,
  start: Dayjs,
  end: Dayjs,
) => {
  const options = rrule.RRule.parseString(rruleString);
  options.dtstart = start.startOf('day').toDate();
  options.until = end.startOf('day').add(1, 'day').toDate();

  const rule = new rrule.RRule(options);

  return rule.all().some((date) => {
    console.log(dayjs(date.toISOString().split('T')[0]).format('YYYY-MM-DD'));
    console.log(shift.format('YYYY-MM-DD'));
    return dayjs(date.toISOString().split('T')[0]).isSame(shift);
  });
};
