import { expect, test, type Page } from '@playwright/test';

import { FakeDevice } from './device';
import { owner } from './global-setup';

type ScreenTime = { bankedSeconds: number; unlockedUntil?: string };

// A household's whole day on the packaged binary, nothing stubbed: the wall display and a child's
// iPad are paired, the child does a chore on the wall, an adult approves it in the inbox, the
// points land, the child spends them on a reward (an adult says yes) and on iPad time (banked at
// once, spent from the iPad, ended early with the rest refunded) and on TV time, which they
// watch on a TV that asked from the wall and stop when they're done.
test('a household’s day: chores, approval, points, rewards, screen time, TV', async ({ browser, playwright, baseURL }) => {
  test.setTimeout(60_000); // the whole day, across four screens
  const problems: string[] = [];
  const watch = (page: Page) => {
    page.on('console', (m) => {
      // Chrome logs every 4xx (a display not yet paired gets 401 from /household) and the request
      // the test drops on purpose (net::ERR_FAILED).
      if (m.type() === 'error' && !/status of 40[14]|net::ERR_FAILED/.test(m.text())) problems.push(m.text());
    });
    page.on('pageerror', (e) => problems.push(e.message));
  };
  const run = String(Date.now()).slice(-6); // reruns against the same database stay apart
  const child = `Robin ${run}`;
  const chore = `Feed the cat ${run}`;
  const game = `Choose a game ${run}`;
  const ipadTime = `20 min of iPad ${run}`;
  const tvTime = `20 min of TV ${run}`;
  const tvName = `Den TV ${run}`;

  const adult = await (await browser.newContext()).newPage();
  const wall = await (await browser.newContext()).newPage();
  watch(adult);
  watch(wall);

  // The wall display asks for a code; an adult signs in and enters it.
  await wall.goto('/display');
  const code = wall.getByLabel(/^Pairing code/);
  await expect(code).toHaveText(/^[A-Z2-9]{6}$/);
  const shown = (await code.textContent()) ?? '';
  await adult.goto('/login');
  await adult.getByLabel('Email').fill(owner.email);
  await adult.getByLabel('Password').fill(owner.password);
  // "Sign in with password" when single sign-on is on (the e2e build), else "Sign in".
  await adult.getByRole('button', { name: /^Sign in( with password)?$/ }).click();
  await expect(adult.getByRole('heading', { name: 'The Example Family' })).toBeVisible();
  await adult.goto('/displays');
  await adult.getByRole('button', { name: 'Pair a display' }).first().click();
  await adult.getByLabel('Code on the display').fill(shown);
  await adult.getByLabel('Name').fill(`Kitchen ${run}`);
  await adult.getByLabel('TV', { exact: true }).check();
  await adult.getByRole('button', { name: 'Pair display' }).click();
  await expect(adult.getByRole('list', { name: 'Paired displays' })).toContainText(`Kitchen ${run}`);
  await expect(wall.getByText('The Example Family')).toBeVisible({ timeout: 15_000 });

  // A child, then their iPad, paired while the sign-in is fresh (pairing needs a recent one).
  await adult.goto('/children');
  await adult.getByRole('button', { name: 'Add a child' }).first().click();
  await adult.getByRole('dialog', { name: 'Add a child' }).getByLabel("Child's name").fill(child);
  await adult.getByRole('button', { name: 'Add child' }).click();
  await expect(adult.getByRole('list', { name: 'Children' })).toContainText(child);
  await adult.goto('/devices');
  await adult.getByRole('button', { name: 'Pair a device' }).click();
  await adult.getByRole('button', { name: child }).click();
  const sheet = adult.getByRole('dialog', { name: `Pair a device for ${child}` });
  const pairing = (await sheet.getByLabel(/^Code /).textContent()) ?? '';
  const ipad = new FakeDevice(await playwright.request.newContext({ baseURL }));
  await ipad.pair(pairing, `${child}’s iPad`);
  await expect(adult.getByText(`${child}’s iPad is paired.`)).toBeVisible({ timeout: 10_000 });
  const children = (await (await adult.request.get('/api/children')).json()) as { items: { id: string; name: string }[] };
  const childId = children.items.find((c) => c.name === child)?.id ?? '';
  const screenTime = async () => {
    const r = await ipad.signed('GET', `/api/children/${childId}/screen-time`);
    expect(r.status()).toBe(200);
    return (await r.json()) as ScreenTime;
  };

  // A chore and two rewards: a game (an adult says yes) and iPad time (given at once).
  await adult.goto('/chores');
  await adult.getByRole('button', { name: 'Add chore' }).first().click();
  const addChore = adult.getByRole('dialog', { name: 'Add a chore' });
  // A segmented control for up to five children, a menu beyond that.
  const forWhom = addChore.getByRole('group', { name: 'For' });
  if (await forWhom.count()) await forWhom.getByRole('button', { name: child }).click();
  else await addChore.getByLabel('For').selectOption({ label: child });
  await addChore.getByRole('textbox', { name: 'Chore', exact: true }).fill(chore);
  await addChore.getByLabel('Points').fill('8');
  await addChore.getByRole('button', { name: 'Add chore' }).click();
  await expect(addChore).toBeHidden();
  await expect(adult.getByRole('list', { name: `${child}’s chores` })).toContainText(chore);
  await adult.goto('/rewards');
  const addReward = adult.getByRole('dialog', { name: 'Add a reward' });
  await adult.getByRole('button', { name: 'Add reward' }).first().click();
  await addReward.getByRole('textbox', { name: 'Reward', exact: true }).fill(game);
  await addReward.getByLabel('Cost in points').fill('3');
  await addReward.getByRole('button', { name: 'Add reward' }).click();
  await expect(addReward).toBeHidden();
  await expect(adult.getByRole('list', { name: 'Rewards' })).toContainText(game);
  await adult.getByRole('button', { name: 'Add reward' }).first().click();
  await addReward.getByRole('textbox', { name: 'Reward', exact: true }).fill(ipadTime);
  await addReward.getByLabel('Cost in points').fill('2');
  await addReward.getByLabel('Kind').selectOption('screen_time');
  await addReward.getByLabel('Minutes').fill('20');
  await addReward.getByRole('button', { name: 'Add reward' }).click();
  await expect(addReward).toBeHidden();
  await expect(adult.getByRole('list', { name: 'Rewards' })).toContainText(ipadTime);
  // TV time, and a TV (the Rewards form has no TV target yet: through the API).
  const key = () => ({ 'Idempotency-Key': crypto.randomUUID() });
  expect((await adult.request.post('/api/rewards', { headers: key(), data: { title: tvTime, cost: 2, kind: 'screen_time', minutes: 20, target: 'tv' } })).status()).toBe(201);
  const madeTv = await adult.request.post('/api/tvs', { headers: key(), data: { name: tvName, macs: [`02:00:5e:${run.slice(0, 2)}:${run.slice(2, 4)}:${run.slice(4, 6)}`] } });
  expect(madeTv.status(), await madeTv.text()).toBe(201);

  // The child ticks the chore on the wall; it waits for an adult, who approves it in the inbox.
  await wall.reload();
  const card = wall.getByRole('article', { name: `${child}'s today` });
  await wall.getByRole('checkbox', { name: chore }).click();
  await expect(wall.getByRole('checkbox', { name: chore })).toHaveAttribute('aria-checked', 'true');
  await expect(card.locator('.oc-points-badge')).toHaveText('0 points');
  await adult.goto('/inbox');
  await adult.getByRole('button', { name: `Approve: ${chore} (${child})` }).click();
  await expect(adult.getByRole('button', { name: `Approve: ${chore} (${child})` })).toHaveCount(0);
  await wall.reload();
  await expect(card.locator('.oc-points-badge')).toHaveText('8 points');

  // On the wall the child asks for the game. The first answer is lost after the server has spent
  // the points; asking again must not spend them twice.
  await wall.getByRole('link', { name: 'Rewards' }).click();
  await wall.getByRole('button', { name: new RegExp(`^${child}`) }).click();
  await wall.getByRole('button', { name: new RegExp(game) }).click();
  await wall.route('**/api/children/*/redemptions', async (route) => {
    await route.fetch();
    await route.abort('failed');
  }, { times: 1 });
  await wall.getByRole('button', { name: 'Ask for it' }).click();
  await expect(wall.getByText('Couldn’t reach the server. Check the connection and try again.')).toBeVisible();
  await wall.getByRole('button', { name: 'Ask for it' }).click();
  await expect(wall.getByText(`Asked for ${game}`)).toBeVisible();
  await adult.goto('/inbox');
  await expect(adult.getByRole('button', { name: `Approve: ${game} (${child})` })).toHaveCount(1);
  await adult.getByRole('button', { name: `Approve: ${game} (${child})` }).click();
  await expect(adult.getByRole('button', { name: `Approve: ${game} (${child})` })).toHaveCount(0);

  // On their iPad's kid view the child buys iPad time: no approval, banked at once.
  const link = await ipad.signed('POST', '/api/device/web-session', '{}');
  expect(link.status()).toBe(201);
  const kid = await (await browser.newContext()).newPage();
  watch(kid);
  await kid.goto(((await link.json()) as { url: string }).url);
  await expect(kid.getByRole('heading', { name: new RegExp(`, ${child}$`) })).toBeVisible();
  await kid.getByRole('link', { name: 'Rewards' }).click();
  await kid.getByRole('button', { name: new RegExp(ipadTime) }).click();
  await kid.getByRole('button', { name: 'Ask for it' }).click();
  await expect.poll(async () => (await screenTime()).bankedSeconds).toBe(1200);
  await expect(adult.getByRole('button', { name: `Approve: ${ipadTime} (${child})` })).toHaveCount(0);
  await wall.goto('/display');
  await expect(card.locator('.oc-points-badge')).toHaveText('3 points');

  // The iPad starts 10 of the 20 minutes; an adult ends it early and refunds what's unused.
  const spend = await ipad.signed('POST', `/api/children/${childId}/screen-time/spend`, JSON.stringify({ minutes: 10 }));
  expect(spend.status(), await spend.text()).toBe(200);
  const running = await screenTime();
  expect(running.bankedSeconds).toBe(600);
  expect(Date.parse(running.unlockedUntil ?? '')).toBeGreaterThan(Date.now());
  const end = await adult.request.post(`/api/children/${childId}/screen-time/end`, {
    headers: { 'Idempotency-Key': crypto.randomUUID() },
    data: { scope: 'now', refund: true },
  });
  expect(end.status(), await end.text()).toBe(200);
  const ended = (await end.json()) as { refundedSeconds: number };
  expect(ended.refundedSeconds).toBeGreaterThan(590);
  expect(ended.refundedSeconds).toBeLessThanOrEqual(600);
  const after = await screenTime();
  expect(after.bankedSeconds).toBe(600 + ended.refundedSeconds);
  expect(after.unlockedUntil === undefined || Date.parse(after.unlockedUntil) <= Date.now()).toBe(true);

  // TV time from a reward: on the kid view, added to the child's TV time at once.
  const tvLeft = async () => {
    const r = await ipad.signed('GET', `/api/children/${childId}/tv`);
    expect(r.status()).toBe(200);
    return ((await r.json()) as { secondsRemaining: number }).secondsRemaining;
  };
  await kid.getByRole('link', { name: 'Rewards' }).click();
  await kid.getByRole('button', { name: new RegExp(tvTime) }).click();
  await kid.getByRole('button', { name: 'Ask for it' }).click();
  await expect.poll(tvLeft).toBe(1200);
  await wall.goto('/display');
  await expect(card.locator('.oc-points-badge')).toHaveText('1 point');

  // The TV is turned on: the wall's TV tab asks for it, the child starts their time on their
  // iPad, and stops it when they're done. The TV is online only in between.
  const session = async () => {
    const r = (await (await adult.request.get('/api/tvs')).json()) as { tvs: { name: string; session: unknown }[] };
    return r.tvs.find((t) => t.name === tvName)?.session ?? null;
  };
  await wall.getByRole('link', { name: 'TV' }).click();
  await wall.getByRole('listitem').filter({ hasText: tvName }).getByRole('button', { name: 'Ask for this TV' }).click();
  await expect(wall.getByText('Asking who’s watching. Answer on your iPad, or ask a parent.')).toBeVisible();
  await kid.goto('/kid');
  await kid.getByRole('button', { name: new RegExp(`^Watch ${tvName}`) }).click();
  await expect(kid.getByRole('heading', { name: `Watching ${tvName}` })).toBeVisible();
  await expect.poll(session).not.toBeNull();
  // Charged by the whole second: watch for more than one before stopping.
  const started = Date.parse(((await session()) as { startedAt: string }).startedAt);
  await expect.poll(() => Date.now() - started).toBeGreaterThan(1_500);
  await kid.getByRole('button', { name: 'I’m done' }).click();
  await expect(kid.getByRole('heading', { name: `Watching ${tvName}` })).toHaveCount(0);
  await expect.poll(session).toBeNull();
  const left = await tvLeft();
  expect(left).toBeGreaterThan(1100);
  expect(left).toBeLessThan(1200);

  expect(problems).toEqual([]);
});
