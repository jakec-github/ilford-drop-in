import { getServiceVolunteers } from './services.js';

const main = async () => {
  const volunteers = await getServiceVolunteers();

  console.log(volunteers);
};

main();
