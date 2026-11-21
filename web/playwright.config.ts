import { randomBytes } from 'node:crypto';

import { defineConfig, devices } from '@playwright/test';

import { oidcClientID, oidcClientSecret, oidcIssuer, oidcName } from './e2e/oidc';

// Runs against the production binary (`make build`, SPA embedded, strict CSP), not the Vite dev
// server, so the test sees what users get. DATABASE_URL must point at a disposable database.
const port = process.env.E2E_PORT ?? '18080';
// A throwaway keyring for the server's encrypted secrets (calendar links), new on every run and
// never written anywhere. The server is started from this process, so this is the key it gets.
const secretKeys = `e2e:${randomBytes(32).toString('base64')}`;

export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://localhost:${port}`,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: '../bin/app serve',
    url: `http://localhost:${port}/readyz`,
    reuseExistingServer: false,
    timeout: 30_000,
    env: {
      LISTEN_ADDR: `127.0.0.1:${port}`,
      LOG_LEVEL: 'warn',
      DATABASE_URL: process.env.DATABASE_URL ?? '',
      // Short, so reauth.spec.ts can see a sign-in go stale.
      RECENT_AUTH_WINDOW: '15s',
      OPENCOZY_SECRET_KEYS: secretKeys,
      // bin/app is an e2e build (make build-e2e); it refuses to start without this.
      OPENCOZY_E2E_BUILD: '1',
      // Single sign-on against e2e/oidc.ts's fake provider; its redirect comes back here.
      PUBLIC_ORIGIN: `http://localhost:${port}`,
      OIDC_ISSUER: oidcIssuer,
      OIDC_CLIENT_ID: oidcClientID,
      OIDC_CLIENT_SECRET: oidcClientSecret,
      OIDC_NAME: oidcName,
    },
  },
});
