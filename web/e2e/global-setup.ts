import { execFileSync } from 'node:child_process';

// There is no sign-up: the e2e owner is made with the CLI, as a real installation's would be.
// A rerun against the same database finds the owner already there.
export const owner = { email: 'e2e-owner@example.test', password: 'a long enough password' };

export default function globalSetup() {
  const env = { ...process.env, LOG_LEVEL: 'warn', DATABASE_URL: process.env.DATABASE_URL ?? '' };
  execFileSync('../bin/app', ['migrate'], { env, stdio: 'inherit' });
  try {
    execFileSync(
      '../bin/app',
      ['bootstrap-owner', '-household', 'The Example Family', '-time-zone', 'Europe/Amsterdam', '-email', owner.email, '-name', 'Sam'],
      { env, input: `${owner.password}\n`, stdio: ['pipe', 'ignore', 'pipe'] },
    );
  } catch (e) {
    const stderr = String((e as { stderr?: Buffer }).stderr ?? '');
    if (!stderr.includes('already exists')) throw e;
  }
}
