import { createMimeMessage } from 'mimetext';

export const createEmail = (
  sender: string,
  recipient: string,
  subject: string,
  body: string,
) => {
  const msg = createMimeMessage();
  msg.setSender({ addr: sender });
  msg.setRecipient(recipient);
  msg.setSubject(subject);
  msg.addMessage({
    contentType: 'text/plain',
    data: body,
  });

  return encodeURI(btoa(msg.asRaw()));
};
