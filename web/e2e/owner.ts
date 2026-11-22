import { execFileSync } from 'node:child_process';

// The e2e owner, made with the CLI in global-setup.ts (and release-setup.ts).
export const owner = { email: 'e2e-owner@example.test', password: 'a long enough password' };

// makeOwner migrates the database and makes the owner with the CLI, as a real installation's
// would be (there is no sign-up). A rerun against the same database finds the owner already there.
export function makeOwner(bin: string, env: NodeJS.ProcessEnv) {
  execFileSync(bin, ['migrate'], { env, stdio: 'inherit' });
  try {
    execFileSync(
      bin,
      ['bootstrap-owner', '-household', 'The Example Family', '-time-zone', 'Europe/Amsterdam', '-email', owner.email, '-name', 'Sam'],
      { env, input: `${owner.password}\n`, stdio: ['pipe', 'pipe', 'pipe'] },
    );
  } catch (e) {
    // The app logs (to stdout or stderr) that a household already exists on a rerun.
    const { stdout, stderr } = e as { stdout?: Buffer; stderr?: Buffer };
    if (!`${String(stdout ?? '')}${String(stderr ?? '')}`.includes('already exists')) throw e;
  }
}
