import { createMimeMessage } from 'mimetext';

import { getMailClient } from '../client.js';
import { ServiceVolunteer } from '../types.js';
import { getConfidentialData } from '../utils/getConfidentialData.js';
import { guardService } from '../utils/guardService.js';

export interface FormWithVolunteer {
  volunteer: ServiceVolunteer;
  formURL: string;
}

const bulkEmailFormPrivate = async (
  forms: FormWithVolunteer[],
  deadline: string,
) => {
  const client = await getMailClient();
  const confidentialData = await getConfidentialData();

  const requests = forms.map((form) =>
    client.users.messages.send({
      userId: confidentialData.gmailUserID,
      requestBody: {
        raw: createEmail(confidentialData.gmailSender, form, deadline),
      },
    }),
  );

  return (await Promise.all(requests)).map(({ status }, i) => ({
    success: status === 200,
    status,
    volunteer: `${forms[i].volunteer.firstName} ${forms[i].volunteer.lastName}`,
  }));
};

export const bulkEmailForm = guardService(
  bulkEmailFormPrivate,
  'Bulk email form',
  false,
);

const createEmail = (
  sender: string,
  { volunteer, formURL: formUrl }: FormWithVolunteer,
  deadline: string,
) => {
  const body = `Hey ${volunteer.firstName}

Please use this form to submit the shifts that you CANNOT do.
${formUrl}

Deadline for responses is ${deadline} when we will create the rota.
You can change your answers as many times as you like before the deadline.
If you attend as part of a group only one member needs to complete the form.

Thanks

The Ilford drop-in team
`;

  const msg = createMimeMessage();
  msg.setSender({ addr: sender });
  msg.setRecipient(volunteer.email);
  msg.setSubject(
    `Ilford drop-in availability (please complete by ${deadline})`,
  );
  msg.addMessage({
    contentType: 'text/plain',
    data: body,
  });

  return encodeURI(btoa(msg.asRaw()));
};
