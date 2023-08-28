import yargs from 'yargs';

import { context } from './context.js';
import { createForms } from './commands/createForms.js';
import { generateRota } from './commands/generateRota.js';
import { sendForms } from './commands/sendForms.js';
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
      },
    },
    ({ shift_count }) => {
      createForms(shift_count);
    },
  )
  .command('send_forms', 'Email forms to volunteers', {}, () => {
    sendForms();
  })
  .command(
    'generate_rota',
    'Use responses to produce a valid rota',
    {
      ['shift_count']: {
        type: 'number',
        description: 'Number of shifts to ask availability for',
        required: true,
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
