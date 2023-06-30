import { createEmail } from './mail.js';
import {
  createAvailabilityForm,
  emailForm,
  getServiceVolunteers,
} from './services.js';
import { getActiveVolunteers } from './volunteers.js';

const main = async () => {
  const volunteers = await getServiceVolunteers();

  // getActiveVolunteers(volunteers)

  (
    await Promise.all(
      volunteers
        .filter(({ lastName }) => lastName === 'Atherton')
        .map(async (volunteer) => {
          const formUrl = await createAvailabilityForm(volunteer, {
            start: new Date(),
            end: new Date('2023-08-31'),
          });

          return {
            volunteer,
            formUrl,
          };
        }),
    )
  ).forEach(({ volunteer, formUrl }) => {
    emailForm(volunteer, formUrl as string); //TODO add guarding to avoid casting
  });
};

main();
