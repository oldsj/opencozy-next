import { defineConfig, devices } from '@playwright/test';

// e2e/imported.spec.ts only, against the release binary that scripts/import-rehearsal.sh
// (JOURNEY=1) starts on the database it imported. Not the e2e suite: no server is started here,
// and the database is the imported household, not the e2e owner's.
const baseURL = process.env.IMPORTED_URL;
if (!baseURL) throw new Error('IMPORTED_URL: run this through JOURNEY=1 scripts/import-rehearsal.sh');

export default defineConfig({
  testDir: 'e2e',
  testMatch: 'imported.spec.ts',
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: 'list',
  use: { baseURL, trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
