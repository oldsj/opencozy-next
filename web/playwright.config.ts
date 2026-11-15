import { defineConfig, devices } from '@playwright/test';

// Runs against the production binary (`make build`, SPA embedded, strict CSP), not the Vite dev
// server, so the test sees what users get. DATABASE_URL must point at a disposable database.
const port = process.env.E2E_PORT ?? '18080';

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
    },
  },
});
