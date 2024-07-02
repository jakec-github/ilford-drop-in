import { forms_v1 } from 'googleapis';

import { NOT_AVAILABLE_RESPONSE } from '../const.js';
import { getFormsClient } from '../client.js';
import { guardService } from '../utils/guardService.js';

const batchGetFormResponsesPrivate = async (formIds: string[]) => {
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
    parseAvailabilityFormResponseResponseData(res.data.responses),
  );
};

export const batchGetFormResponses = guardService(
  batchGetFormResponsesPrivate,
  'Batch get form responses',
);

const parseAvailabilityFormResponseResponseData = (
  apiResponse: forms_v1.Schema$ListFormResponsesResponse['responses'],
): string[] => {
  const allAnswers = apiResponse?.[0].answers;
  if (!allAnswers) {
    return ['No responses'];
  }

  const answerArray = Object.values(allAnswers);

  // If they only answered one question full availability is infered
  if (answerArray.length === 1) {
    return [];
  }

  const results = [
    ...Object.values(answerArray[0].textAnswers?.answers || []),
    ...Object.values(answerArray[1].textAnswers?.answers || []),
  ];

  // TODO: Remove type coercion
  return results
    .map(({ value }) => value)
    .filter((value) => value !== NOT_AVAILABLE_RESPONSE) as string[];
};
