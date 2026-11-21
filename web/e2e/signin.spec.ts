import { expect, test } from '@playwright/test';

import { owner } from './global-setup';

test('sign in, see the household, sign out', async ({ page }) => {
  // Any CSP violation or script error fails the test. Chrome also logs every 4xx response, and
  // 401s are expected here (signed out, wrong password).
  const problems: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error' && !m.text().includes('status of 401')) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));

  const response = await page.goto('/');
  expect(response?.headers()['content-security-policy']).toContain("script-src 'self'");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel('Email').fill(owner.email);
  await page.getByLabel('Password').fill('the wrong password');
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('alert')).toHaveText('Wrong email or password.');

  await page.getByLabel('Password').fill(owner.password);
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  await page.reload();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto('/');
  await expect(page).toHaveURL(/\/login$/);

  expect(problems).toEqual([]);
});
