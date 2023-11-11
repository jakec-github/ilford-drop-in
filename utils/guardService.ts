import { confirmPrompt } from './confirmPrompt.js';

import { context } from '../context.js';

export const guardService = <A extends any[], R>(
  serviceFunction: (...args: A) => Promise<R>,
  serviceName: string,
  readOnly = true,
): ((...args: A) => Promise<R>) => {
  return async (...args) => {
    if (!readOnly) {
      if (!context.live) {
        console.log('Exiting. To run this service use the --live_run flag');
        process.exit();
      }
      console.log(`Executing ${serviceName}`);
      console.log(`Running with arguments:`);
      console.log(args);
      confirmPrompt('Would you like to proceed?');
    } else {
      console.log(`Executing ${serviceName}`);
    }
    const serviceResponse = await serviceFunction(...args);
    console.log('Results:');
    if (Array.isArray(serviceResponse)) {
      console.table(serviceResponse);
    } else {
      console.log(serviceResponse);
    }
    if (readOnly) {
      confirmPrompt('Would you like to proceed?');
    }

    return serviceResponse;
  };
};
