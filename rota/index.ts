import {
  createAvailabilityForm,
  emailForm,
  getServiceVolunteers,
} from './services.js';
import { getActiveVolunteers } from './volunteers.js';

const main = async () => {
  // const volunteers = await getServiceVolunteers();
  // const volunteersAndForms = await Promise.all(
  //   getActiveVolunteers(volunteers).map(async (volunteer) => {
  //     const formUrl = await createAvailabilityForm(volunteer, {
  //       start: new Date('2023-07-30'),
  //       end: new Date('2023-09-17'),
  //     });
  //     return {
  //       volunteer,
  //       formUrl,
  //     };
  //   }),
  // );
  // volunteersAndForms.forEach(({ volunteer, formUrl }) => {
  //   emailForm(volunteer, formUrl as string); //TODO add guarding to avoid casting
  // });
};

main();
