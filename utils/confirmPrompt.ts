import getPrompter from 'prompt-sync';

const prompt = getPrompter();

export const confirmPrompt = (question: string) => {
  loop: while (true) {
    const response = prompt(`${question} (y/N) `, 'n');

    switch (response.toLowerCase()) {
      case 'n':
      case 'no': {
        console.log('Aborting');
        process.exit();
      }
      case 'y':
      case 'yes':
        console.log('Proceeding');
        break loop;
      default:
        console.log(`Invalid response: ${response}`);
    }
  }
};
