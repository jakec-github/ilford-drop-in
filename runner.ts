import yargs from 'yargs';

import getPrompter from 'prompt-sync';

import { createForms } from './scripts/createForms.js';
import { generateRota } from './scripts/generateRota.js';
import { sendForms } from './scripts/sendForms.js';

const prompt = getPrompter();

const warn = (argv: any) => {
  if (argv.live_run) {
    const response = prompt(
      'You are running live. Do you want to continue? (y/n)',
      'n',
    );

    switch (response.toLowerCase()) {
      case 'n':
      case 'no': {
        console.log('Aborting');
        process.exit();
      }
      case 'y':
      case 'yes':
        console.log('Running live');
        return;
      default:
        console.log(`Invalid response: ${response}`);
        process.exit();
    }
  } else {
    console.log('Dry run only. To execute live pass the --live_run flag');
  }
};

const argv = yargs(process.argv.slice(2))
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
    ({ live_run, shift_count }) => {
      createForms(live_run as boolean, shift_count);
    },
  )
  .command('send_forms', 'Email forms to volunteers', {}, ({ live_run }) => {
    sendForms(live_run as boolean);
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
    ({ live_run, shift_count }) => {
      generateRota(live_run as boolean, shift_count);
    },
  )
  .demandCommand(1)
  .strict()
  .option('live_run', {
    describe: 'Side effects will be executed',
    type: 'boolean',
  }).argv;

console.log(argv);
