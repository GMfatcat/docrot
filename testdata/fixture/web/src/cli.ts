import { program } from 'commander';

program
  .option('-r, --retries <n>', 'how many times to retry', 3)
  .option('--verbose', 'log more');

program.parse();
