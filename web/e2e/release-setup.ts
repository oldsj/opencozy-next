import { makeOwner } from './owner';

// The e2e owner on the release binary (playwright.release.config.ts, make e2e-release). No
// fixture feed or fake OIDC provider: a release build can't reach 127.0.0.1, and
// journey.spec.ts uses neither.
export default function releaseSetup() {
  makeOwner('../bin/app-release', { ...process.env, LOG_LEVEL: 'warn', DATABASE_URL: process.env.DATABASE_URL ?? '' });
}
