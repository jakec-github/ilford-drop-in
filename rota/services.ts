import { readFileSync } from 'node:fs';

import { getFormsClient, getMailClient, getSheetsClient } from './clients.js';
import { createEmail } from './mail.js';

export interface ServiceVolunteer {
  firstName: string;
  lastName: string;
  role: string; // TODO: Make enum values enums
  status: string;
  gender: string;
  email: string;
  groupKey: string | null;
}

interface ConfidentialData {
  spreadsheetID: string;
  serviceVolunteersTab: string;
}

export const getServiceVolunteers = async () => {
  const client = getSheetsClient();

  const confidentialData = JSON.parse(
    readFileSync('./secrets/confidential.json', 'utf8'),
  );

  if (!validateConfidentialData(confidentialData)) {
    throw new Error('Confidential data format unexpected');
  }

  const result = await client.spreadsheets.values.get({
    spreadsheetId: confidentialData.spreadsheetID,
    range: confidentialData.serviceVolunteersTab,
  });

  const spreadsheetData = result.data?.values || [];

  if (!validateData(spreadsheetData)) {
    throw new Error('Spreadsheet data format unexpected');
  }

  return parseVolunteers(spreadsheetData);
};

const parseVolunteers = (raw: string[][]): ServiceVolunteer[] =>
  raw.slice(1).map((volunteer) => ({
    firstName: volunteer[0],
    lastName: volunteer[1],
    role: volunteer[2], // TODO: Make enum values enums
    status: volunteer[3],
    gender: volunteer[4],
    email: volunteer[5],
    groupKey: volunteer[10] || null,
  }));

const validateData = (data: unknown): data is string[][] =>
  Array.isArray(data) &&
  data.every(
    (subArray) =>
      Array.isArray(subArray) &&
      subArray.every((element) => typeof element === 'string'),
  );

const validateConfidentialData = (data: unknown): data is ConfidentialData =>
  typeof (data as ConfidentialData)?.spreadsheetID === 'string' &&
  typeof (data as ConfidentialData).serviceVolunteersTab === 'string';

interface DateRange {
  start: Date;
  end: Date;
}

export const createAvailabilityForm = async (
  volunteer: ServiceVolunteer,
  range: DateRange,
) => {
  const client = await getFormsClient();

  const createResult = await client.forms.create({
    requestBody: {
      info: {
        title: `Unavailability for ${volunteer.firstName} ${volunteer.lastName}`,
      },
    },
  });

  if (createResult.status !== 200) {
    // Believe the google client catches issues (This is a simple safeguard)
    throw new Error(
      `Request to create form for ${volunteer.firstName} ${volunteer.lastName}. Failed with ${createResult.status}`,
    );
  }

  const options = getSundaysInRange(range).map((date) => ({ value: date }));

  const updateResult = await client.forms.batchUpdate({
    formId: createResult.data.formId as string, // TODO: Fix this so there isn't type casting
    requestBody: {
      requests: [
        {
          createItem: {
            item: {
              title: 'Please select the dates you are NOT available',
              questionItem: {
                question: {
                  choiceQuestion: {
                    type: 'CHECKBOX',
                    options,
                  },
                },
              },
            },
            location: {
              index: 0,
            },
          },
        },
      ],
    },
  });

  if (updateResult.status !== 200) {
    // Believe the google client catches issues (This is a simple safeguard)
    throw new Error(
      `Request to update form for ${volunteer.firstName} ${volunteer.lastName}. Failed with ${createResult.status}`,
    );
  }

  console.log(
    volunteer.firstName,
    volunteer.lastName,
    createResult.data.responderUri,
    createResult.data.formId,
  );

  return createResult.data.responderUri;
};

const getSundaysInRange = (range: DateRange) => {
  const sundays: string[] = [];

  // Adjust the start date to the nearest Sunday
  const adjustedStartDate = new Date(range.start);
  adjustedStartDate.setDate(
    adjustedStartDate.getDate() + ((7 - adjustedStartDate.getDay()) % 7),
  );

  // Iterate from the adjusted start date until the end date
  let currentDate = adjustedStartDate;
  while (currentDate <= range.end) {
    sundays.push(currentDate.toDateString());
    currentDate.setDate(currentDate.getDate() + 7); // Increment by 7 days for the next Sunday
  }

  return sundays;
};

const EMAIL_ID = 'jakechorley@googlemail.com';

export const emailForm = async (
  volunteer: ServiceVolunteer,
  formUrl: string,
) => {
  const client = await getMailClient();

  const raw = createEmail(
    EMAIL_ID,
    volunteer.email,
    'Unavailaiblity for <DATES>',
    `Hey ${volunteer.firstName} ${volunteer.lastName}

Please use this form to let us know when you aren't availabe for the upcoming rota

${formUrl}

Thanks

The drop-in team`,
  );

  console.log(`Sending email to ${volunteer.email}`);

  return await client.users.messages.send({
    userId: EMAIL_ID,
    requestBody: {
      raw: raw,
    },
  });
};
