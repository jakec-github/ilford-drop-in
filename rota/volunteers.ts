import { ServiceVolunteer } from './services';

export const getActiveVolunteers = (volunteers: ServiceVolunteer[]) =>
  volunteers.filter(({ status }) => status === 'Active');
