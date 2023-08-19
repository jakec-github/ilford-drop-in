import { confirmPrompt } from './confirmPrompt.js';

export const guardService = <A extends any[], R>(
  serviceFunction: (...args: A) => Promise<R>,
  serviceName: string,
  readOnly = true,
): ((...args: A) => Promise<R>) => {
  return async (...args) => {
    console.log(`Executing ${serviceName}`);
    if (!readOnly) {
      console.log(`Running with arguments:`);
      console.log(args);
      confirmPrompt('Would you like to proceed?');
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
