import { expect, test } from '@playwright/test';

import { FakeDevice } from './device';
import { owner } from './global-setup';

test('pair a child’s iPad, open the kid view, tick a chore, unpair it', async ({ browser, playwright, baseURL }) => {
  const run = String(Date.now()).slice(-6);
  const child = `Juniper ${run}`;
  const chore = `Water the plants ${run}`;
  const adult = await (await browser.newContext()).newPage();
  await adult.goto('/login');
  await adult.getByLabel('Email').fill(owner.email);
  await adult.getByLabel('Password').fill(owner.password);
  await adult.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(adult.getByRole('heading', { name: 'The Example Family' })).toBeVisible();

  // A child with a chore on every day.
  const made = await adult.request.post('/api/children', { headers: { 'Idempotency-Key': crypto.randomUUID() }, data: { name: child, color: 'teal' } });
  expect(made.status()).toBe(201);
  const childId = ((await made.json()) as { id: string }).id;
  expect((await adult.request.post('/api/chores', { headers: { 'Idempotency-Key': crypto.randomUUID() }, data: { childId, title: chore, points: 2 } })).status()).toBe(201);

  // The adult offers a pairing; the iPad takes the code.
  await adult.goto('/devices');
  await expect(adult).toHaveTitle('Devices · OpenCozy');
  await adult.getByRole('button', { name: 'Pair a device' }).click();
  await adult.getByRole('button', { name: child }).click();
  const sheet = adult.getByRole('dialog', { name: `Pair a device for ${child}` });
  await expect(sheet.getByRole('img', { name: `Pairing code for ${child}’s device` })).toBeVisible();
  const code = (await sheet.getByLabel(/^Code /).textContent()) ?? '';
  expect(code).toMatch(/^[A-Z2-9]{4}-[A-Z2-9]{4}$/);
  const api = await playwright.request.newContext({ baseURL });
  const ipad = new FakeDevice(api);
  await ipad.pair(code, `${child}’s iPad`);
  await expect(adult.getByText(`${child}’s iPad is paired.`)).toBeVisible({ timeout: 10_000 });
  await expect(adult.getByRole('list', { name: 'Paired devices' })).toContainText(`${child}’s`);

  // The app opens the kid view with a one-time link.
  const link = await ipad.signed('POST', '/api/device/web-session', '{}');
  expect(link.status()).toBe(201);
  const { url } = (await link.json()) as { url: string };
  const kidContext = await browser.newContext();
  const kid = await kidContext.newPage();
  const problems: string[] = [];
  kid.on('console', (m) => {
    if (m.type() === 'error' && !/status of 40[14]/.test(m.text())) problems.push(m.text());
  });
  await kid.goto(url);
  await expect(kid).toHaveURL(/\/kid$/);
  await expect(kid.getByRole('heading', { name: new RegExp(`^Good (morning|afternoon|evening), ${child}$`) })).toBeVisible();
  await kid.getByRole('checkbox', { name: chore }).click();
  await expect(kid.getByRole('checkbox', { name: chore })).toHaveAttribute('aria-checked', 'true');

  // Screen time: the child asks for more, a parent says 30 minutes in the inbox, the iPad shows
  // the countdown; the parent ends it early from Screen time.
  await expect(kid.getByText('No time saved')).toBeVisible();
  await kid.getByRole('button', { name: 'Ask for more time' }).click();
  await expect(kid.getByText('You asked for more time.')).toBeVisible();
  await adult.goto('/inbox');
  await adult.getByRole('group', { name: `More time for ${child}?` }).getByRole('button', { name: '30 min' }).click();
  await expect(adult.getByRole('group', { name: `More time for ${child}?` })).toHaveCount(0);
  await kid.reload();
  await expect(kid.getByRole('timer')).toHaveText(/^\d+:\d\d left$/);
  await adult.goto('/screen-time');
  const card = adult.getByRole('region', { name: `${child}’s screen time` });
  await expect(card.getByRole('timer')).toBeVisible();
  await card.getByRole('button', { name: 'End now…' }).click();
  await adult.getByRole('alertdialog', { name: `End ${child}’s iPad time now?` }).getByRole('button', { name: 'End now' }).click();
  await expect(card.getByText('Locked')).toBeVisible();
  await kid.reload();
  await expect(kid.getByText(/saved$/)).toBeVisible();
  await adult.goto('/devices');

  await kid.getByRole('link', { name: 'Rewards' }).click();
  await expect(kid.getByRole('heading', { name: 'Rewards' })).toBeVisible();
  await expect(kid.getByText('Who’s choosing?')).toHaveCount(0);

  // The link worked once.
  const again = await (await browser.newContext()).newPage();
  await again.goto(url);
  await expect(again).toHaveURL(/\/device\/expired$/);
  await expect(again.getByText('One moment…')).toBeVisible();

  // The kid view's session ends (a dead device cookie): the page goes to /device/expired, which
  // the app takes as its cue to open a new link.
  const before = await kidContext.cookies();
  await kidContext.clearCookies();
  await kidContext.addCookies(before.map((c) => (c.name === '__Host-device' ? { ...c, value: 'x'.repeat(c.value.length) } : c)));
  await kid.reload();
  await expect(kid).toHaveURL(/\/device\/expired$/);
  await expect(kid.getByText('One moment…')).toBeVisible();

  // A fresh link brings the kid view back.
  const relink = await ipad.signed('POST', '/api/device/web-session', '{}');
  expect(relink.status()).toBe(201);
  await kid.goto(((await relink.json()) as { url: string }).url);
  await expect(kid).toHaveURL(/\/kid$/);
  await expect(kid.getByRole('heading', { name: new RegExp(`, ${child}$`) })).toBeVisible();

  // Unpairing ends it at once.
  await adult.getByRole('button', { name: `Unpair ${child}’s iPad` }).click();
  await adult.getByRole('alertdialog').getByRole('button', { name: `Unpair ${child}’s iPad` }).click();
  await expect(adult.getByText(`${child}’s iPad`, { exact: true })).toHaveCount(0);
  expect((await ipad.signed('GET', '/api/device')).status()).toBe(401);
  await kid.reload();
  await expect(kid).toHaveURL(/\/device\/expired$/);
  expect(problems).toEqual([]);
});
