import { createAvailabilityForm, getServiceVolunteers } from './services.js';
import { getActiveVolunteers } from './volunteers.js';

const main = async () => {
  const volunteers = await getServiceVolunteers();

  getActiveVolunteers(volunteers).forEach((volunteer) => {
    createAvailabilityForm(volunteer, {
      start: new Date(),
      end: new Date('2023-08-31'),
    });
  });
};

main();
