import yargs from 'yargs';
import { createForms } from './scripts/createForms.js';
import { generateRota } from './scripts/generateRota.js';
import { sendForms } from './scripts/sendForms.js';

const argv = yargs(process.argv.slice(2))
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
