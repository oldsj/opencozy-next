import { expect, test } from '@playwright/test';

test('sign up, manage notes, sign out and back in', async ({ page }) => {
  // Any CSP violation or script error fails the test. Chrome also logs every 4xx response, and
  // 401s are expected here (signed out, wrong password).
  const problems: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error' && !m.text().includes('status of 401')) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));

  const email = `e2e-${String(Date.now())}-${String(Math.random()).slice(2, 8)}@example.com`;
  const password = 'a long enough password';

  const response = await page.goto('/');
  expect(response?.headers()['content-security-policy']).toContain("script-src 'self'");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByRole('button', { name: /sign up/i }).click();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign up' }).click();
  await expect(page.getByRole('heading', { name: 'Notes' })).toBeVisible();

  const newNote = page.getByRole('form', { name: 'New note' });
  await newNote.getByLabel('Title').fill('Groceries');
  await newNote.getByLabel('Body').fill('eggs\nflour');
  await newNote.getByRole('button', { name: 'Add note' }).click();
  await expect(page.getByRole('heading', { name: 'Groceries' })).toBeVisible();

  await page.getByRole('button', { name: 'Edit' }).click();
  const edit = page.getByRole('form', { name: 'Edit Groceries' });
  await edit.getByLabel('Title').fill('Shopping');
  await edit.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('heading', { name: 'Shopping' })).toBeVisible();

  await page.reload();
  await expect(page.getByRole('heading', { name: 'Shopping' })).toBeVisible();

  await page.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('No notes yet.')).toBeVisible();

  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto('/');
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('the wrong password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toHaveText('Wrong email or password.');

  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Notes' })).toBeVisible();

  expect(problems).toEqual([]);
});
