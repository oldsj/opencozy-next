import { expect, test } from '@playwright/test';

import { feedURL } from './feed';
import { owner } from './global-setup';

// An adult follows a calendar link, its events show in the week, and one lesson is for a child.
test('add a calendar link, see its events, say who one is for', async ({ page }) => {
  test.setTimeout(90_000);
  const problems: string[] = [];
  page.on('pageerror', (e) => problems.push(e.message));
  page.on('console', (m) => {
    if (m.type() === 'error' && !/status of 40[1349]/.test(m.text())) problems.push(m.text());
  });
  const run = `pool-${String(Date.now()).slice(-6)}`;
  const child = `Robin ${run}`;
  const calendar = `Swim club ${run}`;
  const lesson = `Swimming ${run}`;

  await page.goto('/login');
  await page.getByLabel('Email').fill(owner.email);
  await page.getByLabel('Password').fill(owner.password);
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  await page.goto('/children');
  await page.getByRole('button', { name: 'Add a child' }).first().click();
  await page.getByRole('dialog', { name: 'Add a child' }).getByLabel("Child's name").fill(child);
  await page.getByRole('button', { name: 'Add child' }).click();
  await expect(page.getByRole('list', { name: 'Children' })).toContainText(child);

  await page.goto('/calendars');
  await page.getByRole('button', { name: 'Add a calendar link' }).click();
  const sheet = page.getByRole('dialog', { name: 'Add a calendar link' });
  await sheet.getByRole('textbox', { name: 'Name', exact: true }).fill(calendar);
  await sheet.getByLabel('Calendar address').fill(feedURL(run));
  await sheet.getByRole('button', { name: 'Add calendar' }).click();
  await expect(page.getByRole('list', { name: 'Calendars' })).toContainText(calendar);

  // Adding a link starts a refresh; the lesson shows once the feed is fetched.
  await page.getByRole('navigation', { name: 'Adult management' }).getByRole('link', { name: 'Calendar' }).click();
  await expect(async () => {
    await page.reload();
    await expect(page.getByRole('link', { name: lesson })).toHaveCount(1, { timeout: 2_000 });
  }).toPass({ timeout: 45_000 });

  // Tomorrow's lesson only is for the child.
  await page.getByRole('link', { name: lesson }).click();
  await page.getByRole('button', { name: 'Change who it’s for' }).click();
  const forChild = page.getByRole('checkbox', { name: `${lesson} is for ${child}` });
  await forChild.click();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  const scope = page.getByRole('dialog', { name: `Save who ${lesson} is for?` });
  await scope.getByRole('button', { name: 'Just this one' }).click();
  await scope.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(scope).toBeHidden();
  await page.reload();
  await expect(forChild).toBeChecked();

  await page.getByRole('link', { name: 'Back to calendar' }).click();
  await expect(page.getByText(`for ${child}`)).toHaveCount(1);

  // Removing the link removes its events.
  await page.goto('/calendars');
  await page.getByRole('button', { name: `Edit ${calendar}` }).click();
  await page.getByRole('button', { name: 'Remove…' }).click();
  const confirm = page.getByRole('alertdialog', { name: `Remove ${calendar}?` });
  await confirm.getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByRole('list', { name: 'Calendars' }).getByText(calendar)).toHaveCount(0);

  expect(problems).toEqual([]);
});
