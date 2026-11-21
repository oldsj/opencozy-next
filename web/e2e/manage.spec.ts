import { expect, test, type APIRequestContext } from '@playwright/test';

import { owner } from './global-setup';

const key = () => ({ 'Idempotency-Key': crypto.randomUUID() });

async function post<T>(request: APIRequestContext, path: string, data: unknown): Promise<T> {
  const r = await request.post(path, { headers: key(), data });
  expect(r.status(), `${path}: ${await r.text()}`).toBeLessThan(300);
  return (await r.json()) as T;
}

// Editing in Manage: a chore given to another child, a reward hidden, a chosen reward given or
// cancelled with its points back, and the household's time zone checked.
test('manage: edit and reassign, hide, give and cancel, household', async ({ page }) => {
  const problems: string[] = [];
  page.on('console', (m) => {
    // Chrome logs every 4xx (the household form gets a 422 and a stale cancel a 409 on purpose),
    // and the reset of the save whose answer the test drops.
    if (m.type() === 'error' && !/status of 4\d\d|ERR_CONNECTION_RESET/.test(m.text())) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));
  const run = String(Date.now()).slice(-6);
  const ada = `Ada ${run}`;
  const noor = `Noor ${run}`;

  await page.goto('/login');
  await page.getByLabel('Email').fill(owner.email);
  await page.getByLabel('Password').fill(owner.password);
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  // Two children; Ada earns 14 points (an adult's tick counts at once) and has a chore to move.
  const a = await post<{ id: string }>(page.request, '/api/children', { name: ada, color: 'teal' });
  await post(page.request, '/api/children', { name: noor, color: 'rose' });
  const earned = await post<{ id: string }>(page.request, '/api/chores', { childId: a.id, title: `Water the plants ${run}`, points: 14 });
  await post(page.request, `/api/today/chores/${earned.id}/complete`, undefined);
  await post(page.request, '/api/chores', { childId: a.id, title: `Tidy the hallway ${run}`, points: 2 });
  const reward = await post<{ id: string }>(page.request, '/api/rewards', { title: `Pick the film ${run}`, cost: 4, kind: 'item' });

  // The chore goes to Noor, worth more.
  await page.goto('/chores');
  await page.getByRole('button', { name: `Edit Tidy the hallway ${run}` }).click();
  const sheet = page.getByRole('dialog', { name: `Edit Tidy the hallway ${run}` });
  const whose = sheet.getByRole('group', { name: 'Whose chore' });
  if (await whose.count()) await whose.getByRole('button', { name: noor }).click();
  else await sheet.getByLabel('Whose chore').selectOption({ label: noor });
  await expect(sheet).toContainText(`Moving it to ${noor}: it leaves ${ada}’s list today`);
  await sheet.getByLabel('Points').fill('5');
  await sheet.getByRole('button', { name: 'Save' }).click();
  await expect(sheet).toBeHidden();
  await expect(page.getByRole('list', { name: 'Chores' }).getByRole('listitem').filter({ hasText: `Tidy the hallway ${run}` })).toContainText(`${noor} · 5 points`);

  // The reward is hidden from the children.
  await page.goto('/rewards');
  await page.getByRole('button', { name: `Edit Pick the film ${run}` }).click();
  const rsheet = page.getByRole('dialog', { name: `Edit Pick the film ${run}` });
  await rsheet.getByRole('group', { name: 'Children can choose it' }).getByRole('button', { name: 'Hidden' }).click();
  // The first save reaches the server but its answer is lost. The form refetches the newer version,
  // and saving the same fields again must go through, not trip over the old key.
  let lost = false;
  await page.route('**/api/rewards/*', async (route) => {
    if (lost || route.request().method() !== 'PUT') { await route.fallback(); return; }
    lost = true;
    await route.fetch();
    await route.abort('connectionreset');
  });
  await rsheet.getByRole('button', { name: 'Save' }).click();
  await expect.poll(() => lost).toBe(true);
  await expect(rsheet.getByRole('alert')).toBeVisible();
  await rsheet.getByRole('group', { name: 'Children can choose it' }).getByRole('button', { name: 'Hidden' }).click();
  await rsheet.getByRole('button', { name: 'Save' }).click();
  await expect(rsheet).toBeHidden();
  await page.unroute('**/api/rewards/*');
  await expect(page.getByRole('list', { name: 'Rewards' }).getByRole('listitem').filter({ hasText: `Pick the film ${run}` })).toContainText('hidden from children');
  await page.getByRole('button', { name: `Edit Pick the film ${run}` }).click();
  await rsheet.getByRole('group', { name: 'Children can choose it' }).getByRole('button', { name: 'Shown' }).click();
  await rsheet.getByRole('button', { name: 'Save' }).click();
  await expect(rsheet).toBeHidden();

  // An adult chooses it for Ada three times (approved at once): two are given, one cancelled with its points back.
  await post(page.request, `/api/children/${a.id}/redemptions`, { rewardId: reward.id });
  await post(page.request, `/api/children/${a.id}/redemptions`, { rewardId: reward.id });
  await page.getByRole('link', { name: 'Chosen rewards' }).click();
  await expect(page.getByRole('heading', { name: 'Chosen rewards' })).toBeVisible();
  const toGive = page.getByRole('list', { name: 'To give' });
  await expect(toGive.getByRole('listitem').filter({ hasText: ada })).toHaveCount(2);
  await toGive.getByRole('button', { name: `Mark Pick the film ${run} as given to ${ada}` }).first().click();
  await expect(page.getByRole('list', { name: 'Given' }).getByRole('listitem').filter({ hasText: ada })).toHaveCount(1);
  // A third, given from another tab while its cancel dialog is open: the stale cancel is refused,
  // and the dialog closes because a given item can't be cancelled.
  await post(page.request, `/api/children/${a.id}/redemptions`, { rewardId: reward.id });
  await page.reload();
  await expect(toGive.getByRole('listitem').filter({ hasText: ada })).toHaveCount(2);
  await toGive.getByRole('button', { name: `Cancel Pick the film ${run} for ${ada} and refund` }).first().click();
  const open = (await (await page.request.get('/api/redemptions?status=approved')).json()) as { redemptions: { id: string; version: number; child: { name: string } }[] };
  const shown = open.redemptions.filter((r) => r.child.name === ada);
  // The list is newest first, and so is the page: the dialog is for the first one shown.
  await post(page.request, `/api/redemptions/${shown[0].id}/fulfil`, { expectedVersion: shown[0].version });
  await page.getByRole('button', { name: 'Cancel and refund 4 points' }).click();
  await expect(page.getByRole('dialog')).toBeHidden();
  await expect(page.getByRole('list', { name: 'Given' }).getByRole('listitem').filter({ hasText: ada })).toHaveCount(2);
  await expect(toGive.getByRole('listitem').filter({ hasText: ada })).toHaveCount(1);

  await toGive.getByRole('button', { name: `Cancel Pick the film ${run} for ${ada} and refund` }).click();
  await page.getByRole('button', { name: 'Cancel and refund 4 points' }).click();
  await expect(page.getByRole('list', { name: 'Cancelled' }).getByRole('listitem').filter({ hasText: ada })).toContainText('4 points back');
  await expect(page.getByRole('list', { name: 'Given' }).getByRole('button', { name: new RegExp(ada) })).toHaveCount(0);

  // The points came back: 14 earned, 12 spent, 4 back.
  await page.goto(`/children/${a.id}/points`);
  await expect(page.locator('.oc-points-badge')).toHaveText('6 points');

  // The household's time zone must be one from the list.
  await page.goto('/household');
  const zone = page.getByLabel('Time zone');
  const was = await zone.inputValue();
  await zone.fill('EST');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText('Choose a time zone from the list.')).toBeVisible();
  await zone.fill(was);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText('Saved.')).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveValue('The Example Family');

  expect(problems).toEqual([]);
});
