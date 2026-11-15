import { expect, test } from '@playwright/test';

import { owner } from './global-setup';

// An adult sets up learning: an award rule, a player linked to a child, and a learning service's
// key limited to that rule. Then revokes the key.
test('award rules, players and a learning connection', async ({ page }) => {
  test.setTimeout(60_000);
  const problems: string[] = [];
  page.on('pageerror', (e) => problems.push(e.message));
  page.on('console', (m) => {
    if (m.type() === 'error' && !/status of 40[1349]/.test(m.text())) problems.push(m.text());
  });
  const run = String(Date.now()).slice(-6);
  const child = `Kit ${run}`;
  const key = `quest.complete.${run}`;
  const app = `Example Learning App ${run}`;

  await page.goto('/login');
  await page.getByLabel('Email').fill(owner.email);
  await page.getByLabel('Password').fill(owner.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  await page.goto('/chores');
  await page.getByLabel("Child's name").fill(child);
  await page.getByRole('button', { name: 'Add child' }).click();
  await expect(page.getByLabel('For').getByRole('option', { name: child })).toHaveCount(1);

  // A rule, edited; a second rule for the same activity is refused on the sheet.
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Learning' }).click();
  await page.getByRole('button', { name: 'Add a rule' }).click();
  let sheet = page.getByRole('dialog', { name: 'Add a rule' });
  await sheet.getByLabel('Activity key').fill(key);
  await sheet.getByLabel('Points each time').fill('5');
  await sheet.getByLabel('Daily limit, in points').fill('20');
  await sheet.getByRole('button', { name: 'Save rule' }).click();
  const rules = page.getByRole('list', { name: 'Award rules' });
  await expect(rules).toContainText(key);
  await expect(rules).toContainText('up to 20 points a day');

  await page.getByRole('button', { name: `Edit ${key}` }).click();
  sheet = page.getByRole('dialog', { name: 'Edit rule' });
  await sheet.getByLabel('Daily limit, in points').fill('25');
  await sheet.getByRole('button', { name: 'Save rule' }).click();
  await expect(rules).toContainText('up to 25 points a day');

  await page.getByRole('button', { name: 'Add a rule' }).click();
  sheet = page.getByRole('dialog', { name: 'Add a rule' });
  await sheet.getByLabel('Activity key').fill(key);
  await sheet.getByRole('button', { name: 'Save rule' }).click();
  await expect(sheet.getByText('A rule for that activity already exists. Edit it instead.')).toBeVisible();
  await sheet.getByRole('button', { name: 'Cancel' }).click();

  // A player for the child.
  await page.getByRole('button', { name: 'Link a player' }).click();
  sheet = page.getByRole('dialog', { name: 'Link a player' });
  await sheet.getByRole('button', { name: child }).click();
  await sheet.getByLabel('Minecraft player ID').fill(`Builder_${run}`);
  await sheet.getByRole('button', { name: 'Link', exact: true }).click();
  await expect(page.getByRole('list', { name: 'Players' })).toContainText(`${child} is Builder_${run}`);

  // A learning service's key, for that rule only.
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Connections' }).click();
  await page.getByRole('button', { name: 'Add a service' }).click();
  sheet = page.getByRole('dialog', { name: 'Add a connection' });
  await sheet.getByRole('textbox', { name: 'Name', exact: true }).fill(app);
  await sheet.getByRole('checkbox', { name: 'Give points for learning, by your award rules' }).click();
  await sheet.getByRole('checkbox', { name: `Use ${key}` }).click();
  await sheet.getByRole('button', { name: 'Choose children' }).click();
  await sheet.getByRole('checkbox', { name: child }).click();
  await sheet.getByRole('button', { name: 'Create key' }).click();
  const secret = page.getByRole('dialog', { name: `Key for ${app}` });
  const again = page.getByRole('alertdialog', { name: 'Enter your password again' });
  await expect(secret.or(again)).toBeVisible();
  if (await again.isVisible()) {
    await again.getByLabel('Password').fill(owner.password);
    await again.getByRole('button', { name: 'Continue' }).click();
  }
  await expect(secret.getByLabel('Key')).toHaveValue(/^ocz_/);
  await secret.getByRole('button', { name: 'Done' }).click();
  const list = page.getByRole('list', { name: 'Connections' });
  await expect(list).toContainText(app);
  await expect(list).toContainText(`awards ${key}`);

  await page.getByRole('button', { name: `Revoke ${app}` }).click();
  await page.getByRole('alertdialog', { name: `Revoke ${app}?` }).getByRole('button', { name: 'Revoke' }).click();
  await expect(page.getByText(app)).toHaveCount(0);

  expect(problems).toEqual([]);
});
