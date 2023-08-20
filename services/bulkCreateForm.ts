import { getFormsClient } from '../client.js';
import { ServiceVolunteer } from '../types.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';

interface FormData {
  firstName: string;
  lastName: string;
  volunteerID: string;
  formID: string;
  formURL: string;
}

const bulkCreateFormsPrivate = async (
  dates: string[],
  volunteers: ServiceVolunteer[],
) => {
  const client = await getFormsClient();
  const confidentialData = getConfidentialData();

  const formCreations = volunteers.map(
    async ({ firstName, lastName, id }): Promise<FormData> => {
      const createResult = await client.forms.create({
        requestBody: {
          info: {
            title: `Unavailability for ${firstName} ${lastName}`,
          },
        },
      });

      const formID = createResult.data.formId;
      const formURL = createResult.data.responderUri;

      if (typeof formID !== 'string' || typeof formURL !== 'string') {
        throw new Error(
          `Unexpected response: ${formID}/${formURL} for ${firstName} ${lastName} when creating form`,
        );
      }

      await client.forms.batchUpdate({
        formId: formID,
        requestBody: {
          requests: [getFormLayout(dates)],
        },
      });

      return {
        firstName,
        lastName,
        volunteerID: id,
        formID,
        formURL,
      };
    },
  );

  return Promise.all(formCreations);
};

export const bulkCreateForms = guardService(
  bulkCreateFormsPrivate,
  'Create Forms',
  false,
);

const getFormLayout = (dates: string[]) => ({
  createItem: {
    item: {
      title: 'Please select the dates you are NOT available',
      questionItem: {
        question: {
          choiceQuestion: {
            type: 'CHECKBOX',
            options: dates.map((date) => ({ value: date })),
          },
        },
      },
    },
    location: {
      index: 0,
    },
  },
});
