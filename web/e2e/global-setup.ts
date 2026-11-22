import { serveFeeds } from './feed';
import { serveOIDC } from './oidc';
import { makeOwner, owner } from './owner';

export { owner };

// The e2e owner (owner.ts), then the fixture calendar feed and the fake OIDC provider.
export default async function globalSetup() {
  // bin/app is an e2e build (make build-e2e), which refuses to run without OPENCOZY_E2E_BUILD.
  makeOwner('../bin/app', { ...process.env, LOG_LEVEL: 'warn', DATABASE_URL: process.env.DATABASE_URL ?? '', OPENCOZY_E2E_BUILD: '1' });
  const servers = [await serveFeeds(), await serveOIDC()];
  return () => Promise.all(servers.map((s) => new Promise<void>((resolve) => { s.close(() => { resolve(); }); }))).then(() => undefined);
}
