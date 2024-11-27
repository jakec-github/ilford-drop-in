import yargs from 'yargs';

import { addForms } from './commands/addForms.js';
import { checkResponse } from './commands/checkResponse.js';
import { createForms } from './commands/createForms.js';
import { generateRota } from './commands/generateRota.js';
import { sendForms } from './commands/sendForms.js';
import { viewResponses } from './commands/viewResponses.js';
import { context } from './context.js';
import { confirmPrompt } from './utils/confirmPrompt.js';

const warn = (argv: any) => {
  if (argv.live_run) {
    confirmPrompt('You are running live. Do you want to continue?');
    context.live = true;
  } else {
    console.log('Dry run only. To execute live pass the --live_run flag');
  }
};

yargs(process.argv.slice(2))
  .middleware(warn)
  .command(
    'create_forms',
    'Create unavailability forms',
    {
      ['shift_count']: {
        type: 'number',
        description: 'Number of shifts to ask availability for',
        required: true,
        default: 12,
      },
    },
    ({ shift_count }) => {
      createForms(shift_count);
    },
  )
  .command(
    'add_forms',
    'Create additional unavailability forms',
    {
      ['shift_count']: {
        type: 'number',
        description: 'Number of shifts to ask availability for',
        required: true,
        default: 12,
      },
      ['volunteer_ids']: {
        type: 'array',
        description: 'Volunteer IDs to add forms for',
        required: true,
      },
    },
    ({ shift_count, volunteer_ids }) => {
      const volunteerIDs = volunteer_ids?.map((id) => String(id)) || [];
      addForms(shift_count, volunteerIDs);
    },
  )
  .command(
    'send_forms',
    'Email forms to volunteers',
    {
      ['deadline']: {
        type: 'string',
        description:
          'Human readable deadline for form responses "Deadline for responses is ${deadline} when we will create the rota."',
        required: true,
      },
      ['volunteer_ids']: {
        type: 'array',
        description: 'Volunteer IDs to send form to. All volunteers if omitted',
        required: false,
      },
    },
    ({ deadline, volunteer_ids }) => {
      const volunteerIDs = volunteer_ids?.map((id) => String(id));
      sendForms(deadline, volunteerIDs);
    },
  )
  .command(
    'view_responses',
    'See all form responses',
    {
      ['first_shift']: {
        type: 'string',
        description: 'The first shift for the period in 8601',
        required: true,
      },
      ['shift_count']: {
        type: 'number',
        description: 'Number of shifts that will be in the rota',
        required: true,
        default: 12,
      },
    },
    ({ first_shift, shift_count }) => {
      viewResponses(first_shift, shift_count);
    },
  )
  .command(
    'check_response',
    'See a volunteers response',
    {
      ['first_shift']: {
        type: 'string',
        description: 'The first shift for the period in 8601',
        required: true,
      },
      ['volunteer_id']: {
        type: 'string',
        description: 'Volunteer ID to check',
        required: true,
      },
    },
    ({ first_shift, volunteer_id }) => {
      checkResponse(first_shift, volunteer_id);
    },
  )
  .command(
    'generate_rota',
    'Use responses to produce a valid rota',
    {
      ['shift_count']: {
        type: 'number',
        description: 'Number of shifts to create rota for',
        required: true,
        default: 12,
      },
    },
    ({ shift_count }) => {
      generateRota(shift_count);
    },
  )
  .demandCommand(1)
  .strict()
  .option('live_run', {
    describe: 'Side effects will be executed',
    type: 'boolean',
  }).argv;
