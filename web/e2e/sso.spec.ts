import { expect, test } from '@playwright/test';

import { oidcName } from './oidc';

test('sign in with single sign-on, see the link, unlink it', async ({ page }) => {
  // Any CSP violation or script error fails the test (401s are expected while signed out).
  const problems: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error' && !m.text().includes('status of 401')) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));

  await page.goto('/login');
  // The fake provider signs in straight away and sends the browser back to the callback.
  await page.getByRole('button', { name: `Sign in with ${oidcName}` }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  await page.getByRole('link', { name: 'Account' }).first().click();
  await expect(page.getByText(`Linked to ${oidcName}`)).toBeVisible();
  await page.getByRole('button', { name: `Unlink ${oidcName}` }).click();
  await page.getByRole('alertdialog', { name: `Unlink ${oidcName}?` }).getByRole('button', { name: `Unlink ${oidcName}` }).click();
  await expect(page.getByText(/^Not linked\./)).toBeVisible();

  expect(problems).toEqual([]);
});
