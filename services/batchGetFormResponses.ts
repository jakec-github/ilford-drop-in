import dayjs from 'dayjs';
import { forms_v1 } from 'googleapis';

import { getFormsClient } from '../client.js';
import { Availability } from '../types.js';
import { guardService } from '../utils/guardService.js';

const batchGetFormResponsesPrivate = async (
  formIds: string[],
  shiftDates: string[],
) => {
  const client = await getFormsClient();

  const apiResponses = await Promise.all(
    formIds.map((formId) =>
      client.forms.responses.list({
        formId,
        pageSize: 1,
      }),
    ),
  );

  return apiResponses.map((res) =>
    parseAvailabilityFormResponseResponseData(res.data.responses, shiftDates),
  );
};

export const batchGetFormResponses = guardService(
  batchGetFormResponsesPrivate,
  'Batch get form responses',
);

const parseAvailabilityFormResponseResponseData = (
  apiResponse: forms_v1.Schema$ListFormResponsesResponse['responses'],
  shiftDates: string[],
): Availability => {
  const allAnswers = apiResponse?.[0].answers;
  if (!allAnswers) {
    return {
      responded: false,
      days: [],
    };
  }

  const answerArray = Object.values(allAnswers);
  const shiftDays = shiftDates.map((date) => dayjs(date));

  // If they only answered one question full availability is infered
  if (answerArray.length === 1) {
    return {
      responded: true,
      days: shiftDays,
    };
  }

  const results = [
    ...Object.values(answerArray[0].textAnswers?.answers || []),
    ...Object.values(answerArray[1].textAnswers?.answers || []),
  ].map(({ value }) => value);

  return {
    responded: true,
    days: shiftDates
      .filter((shiftDate) => !results.includes(shiftDate))
      .map((date) => dayjs(date)),
  };
};
