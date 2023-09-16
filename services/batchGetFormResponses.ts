import { forms_v1 } from 'googleapis';

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
  const answers = apiResponse?.[0].answers;
  if (!answers) {
    return ['No responses'];
  }

  const results = Object.values(answers)[0].textAnswers?.answers;
  if (!results) {
    return [];
  }

  //TODO: Remove type coercion
  return (Object.values(results).map(({ value }) => value) as string[]) || [];
};
