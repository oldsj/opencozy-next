import { expect, test } from '@playwright/test';

import { owner } from './global-setup';

// Pairing needs a recent sign-in (15 s in the e2e server, 10 minutes for real). Once it has gone
// stale, the app asks for the password again and then pairs, with no second sign-in.
test('pairing after the sign-in has gone stale asks for the password again', async ({ browser }) => {
  test.setTimeout(60_000);
  const run = String(Date.now()).slice(-6);
  const wall = await (await browser.newContext()).newPage();
  const adult = await (await browser.newContext()).newPage();

  await adult.goto('/login');
  await adult.getByLabel('Email').fill(owner.email);
  await adult.getByLabel('Password').fill(owner.password);
  await adult.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(adult.getByRole('heading', { name: 'The Example Family' })).toBeVisible();
  await expect(adult).toHaveTitle('Overview · OpenCozy');
  await expect(adult.getByRole('navigation', { name: 'Adult management' })).toBeVisible();

  await adult.waitForTimeout(16_000);

  await wall.goto('/display');
  const code = wall.getByLabel(/^Pairing code/);
  await expect(code).toHaveText(/^[A-Z2-9]{6}$/);
  await adult.goto('/displays');
  await adult.getByRole('button', { name: 'Pair a display' }).first().click();
  await adult.getByLabel('Code on the display').fill((await code.textContent()) ?? '');
  await adult.getByLabel('Name').fill(`Hallway ${run}`);
  await adult.getByRole('button', { name: 'Pair display' }).click();

  const dialog = adult.getByRole('alertdialog', { name: 'Enter your password again' });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Password').fill(owner.password);
  await dialog.getByRole('button', { name: 'Continue' }).click();
  await expect(adult.getByRole('list', { name: 'Paired displays' })).toContainText(`Hallway ${run}`);
  await expect(wall.getByRole('heading', { name: 'Pair this display' })).toHaveCount(0);
});

test('a session that ends sends the adult to sign in, saying why', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('Email').fill(owner.email);
  await page.getByLabel('Password').fill(owner.password);
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();
  // The overview loads several things at once: let them land, so the click is what finds the
  // session gone rather than one of those first requests.
  await page.waitForLoadState('networkidle');
  await page.context().clearCookies(); // as if it expired, or was signed out elsewhere
  await page.getByRole('navigation', { name: 'Adult management' }).getByRole('link', { name: 'Inbox' }).click();
  await expect(page).toHaveURL(/\/login\?ended=true$/);
  await expect(page.getByText('Your sign-in ended. Sign in again to carry on.')).toBeVisible();
});
