import { execFileSync } from 'node:child_process';

import { serveFeeds } from './feed';
import { serveOIDC } from './oidc';
import { owner } from './owner';

export { owner };

// There is no sign-up: the e2e owner is made with the CLI, as a real installation's would be.
// A rerun against the same database finds the owner already there.

export default async function globalSetup() {
  // bin/app is an e2e build (make build-e2e), which refuses to run without OPENCOZY_E2E_BUILD.
  const env = { ...process.env, LOG_LEVEL: 'warn', DATABASE_URL: process.env.DATABASE_URL ?? '', OPENCOZY_E2E_BUILD: '1' };
  execFileSync('../bin/app', ['migrate'], { env, stdio: 'inherit' });
  try {
    execFileSync(
      '../bin/app',
      ['bootstrap-owner', '-household', 'The Example Family', '-time-zone', 'Europe/Amsterdam', '-email', owner.email, '-name', 'Sam'],
      { env, input: `${owner.password}\n`, stdio: ['pipe', 'pipe', 'pipe'] },
    );
  } catch (e) {
    // The app logs (to stdout or stderr) that a household already exists on a rerun.
    const { stdout, stderr } = e as { stdout?: Buffer; stderr?: Buffer };
    if (!`${String(stdout ?? '')}${String(stderr ?? '')}`.includes('already exists')) throw e;
  }
  const servers = [await serveFeeds(), await serveOIDC()];
  return () => Promise.all(servers.map((s) => new Promise<void>((resolve) => { s.close(() => { resolve(); }); }))).then(() => undefined);
}
