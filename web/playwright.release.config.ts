import { randomBytes } from 'node:crypto';

import { defineConfig, devices } from '@playwright/test';

// e2e/journey.spec.ts on the release binary (bin/app-release: embedweb, no e2e tag, as the
// image ships it), on its own fresh database. The journey needs nothing an e2e build adds: the
// e2e tag only lets the calendar and single sign-on clients reach 127.0.0.1, where the other
// specs serve a fixture feed and a fake OIDC provider. No fake clock exists in either build.
const port = process.env.E2E_RELEASE_PORT ?? '18081';
const secretKeys = `e2e:${randomBytes(32).toString('base64')}`;

export default defineConfig({
  testDir: 'e2e',
  testMatch: 'journey.spec.ts',
  globalSetup: './e2e/release-setup.ts',
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: 'list',
  use: {
    baseURL: `http://localhost:${port}`,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: '../bin/app-release serve',
    url: `http://localhost:${port}/readyz`,
    reuseExistingServer: false,
    timeout: 30_000,
    env: {
      LISTEN_ADDR: `127.0.0.1:${port}`,
      LOG_LEVEL: 'warn',
      DATABASE_URL: process.env.DATABASE_URL ?? '',
      OPENCOZY_SECRET_KEYS: secretKeys,
    },
  },
});
