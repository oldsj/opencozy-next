import { expect, test, type Page } from '@playwright/test';

import { owner } from './global-setup';

// The core chore loop, grown slice by slice: pair the wall display, a child marks a chore done
// (and, in later slices, an adult approves it, points land and the child redeems a reward).
test('pair the wall display, a child marks a chore done', async ({ browser }) => {
  const problems: string[] = [];
  const watch = (page: Page) => {
    page.on('console', (m) => {
      // Chrome logs every 4xx; a display not yet paired gets 401 from /household.
      if (m.type() === 'error' && !/status of 40[14]/.test(m.text())) problems.push(m.text());
    });
    page.on('pageerror', (e) => problems.push(e.message));
  };
  const run = String(Date.now()).slice(-6); // reruns against the same database stay apart
  const child = `Robin ${run}`;
  const chore = `Feed the cat ${run}`;

  const adult = await (await browser.newContext()).newPage();
  const wall = await (await browser.newContext()).newPage();
  watch(adult);
  watch(wall);

  // The display asks for a code.
  await wall.goto('/display');
  await expect(wall.getByRole('heading', { name: 'Pair this display' })).toBeVisible();
  const code = wall.getByLabel(/^Pairing code/);
  await expect(code).toHaveText(/^[A-Z2-9]{6}$/);
  const shown = (await code.textContent()) ?? '';

  // An adult signs in and enters it.
  await adult.goto('/login');
  await adult.getByLabel('Email').fill(owner.email);
  await adult.getByLabel('Password').fill(owner.password);
  await adult.getByRole('button', { name: 'Sign in' }).click();
  await expect(adult.getByRole('heading', { name: 'The Example Family' })).toBeVisible();
  await adult.goto('/displays');
  await adult.getByLabel('Code on the display').fill(shown);
  await adult.getByLabel('Name').fill(`Kitchen ${run}`);
  await adult.getByRole('button', { name: 'Pair display' }).click();
  await expect(adult.getByRole('list', { name: 'Paired displays' })).toContainText(`Kitchen ${run}`);

  // The display collects its session and shows Today.
  await expect(wall.getByText('The Example Family')).toBeVisible({ timeout: 15_000 });

  // The adult adds a child and a chore.
  await adult.goto('/chores');
  await adult.getByLabel("Child's name").fill(child);
  await adult.getByRole('button', { name: 'Add child' }).click();
  await adult.getByLabel('For').selectOption({ label: child });
  await adult.getByRole('textbox', { name: 'Chore', exact: true }).fill(chore);
  await adult.getByLabel('Points').fill('4');
  await adult.getByRole('button', { name: 'Add chore' }).click();
  await expect(adult.getByRole('list', { name: 'Chores' })).toContainText(chore);

  // The child taps it on the wall display; it waits for approval.
  await wall.reload();
  const check = wall.getByRole('checkbox', { name: chore });
  await expect(check).toHaveAttribute('aria-checked', 'false');
  await check.click();
  await expect(check).toHaveAttribute('aria-checked', 'true');
  await wall.reload();
  await expect(wall.getByRole('checkbox', { name: chore })).toHaveAttribute('aria-checked', 'true');

  expect(problems).toEqual([]);
});
