import { expect, test } from '@playwright/test';

import { owner } from './global-setup';

// An adult adds a recipe, plans it for this week, then swaps it for a plain entry.
test('add a recipe and plan it', async ({ page }) => {
  const problems: string[] = [];
  page.on('pageerror', (e) => problems.push(e.message));
  page.on('console', (m) => {
    if (m.type() === 'error' && !/status of 40[149]/.test(m.text())) problems.push(m.text());
  });
  const run = String(Date.now()).slice(-6);
  const title = `Lentil soup ${run}`;

  await page.goto('/login');
  await page.getByLabel('Email').fill(owner.email);
  await page.getByLabel('Password').fill(owner.password);
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  await page.goto('/recipes');
  await page.getByRole('link', { name: 'Add recipe' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(title);
  await page.getByLabel('Serves').fill('4');
  await page.getByLabel('Ingredients').fill('1 cup red lentils\n1 onion');
  await page.getByRole('textbox', { name: 'Step 1', exact: true }).fill('Soften the onion.\nSlowly.');
  await page.getByRole('button', { name: 'Save recipe' }).click();
  await expect(page.getByRole('heading', { name: title })).toBeVisible();
  await expect(page.getByText('1 onion')).toBeVisible();

  await page.goto('/meals');
  const plan = page.getByRole('button', { name: /^Plan Dinner,/ }).first();
  const slot = ((await plan.getAttribute('aria-label')) ?? '').replace(/^Plan /, '');
  await plan.click();
  await page.getByRole('combobox', { name: 'Meal', exact: true }).fill(title);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  const change = page.getByRole('button', { name: `Change ${slot}` });
  await expect(change).toBeVisible();
  await expect(page.getByText(title)).toBeVisible();

  await change.click();
  await page.getByRole('combobox', { name: 'Meal', exact: true }).fill(`Leftovers ${run}`);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText(`Leftovers ${run}`)).toBeVisible();
  await expect(page.getByText(title)).toHaveCount(0);

  expect(problems).toEqual([]);
});
