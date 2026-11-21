import { expect, test, type APIRequestContext } from '@playwright/test';

import { FakeDevice } from './device';
import { household as h } from './imported-fixture';

// The imported household on the release binary (JOURNEY=1 scripts/import-rehearsal.sh, through
// playwright.imported.config.ts): what the old app's data must still do after cutover. Nothing
// here makes the household; everything comes from the import.
test('the imported household works on the new server', async ({ page, playwright }) => {
  const problems: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error' && !/status of 40[14]/.test(m.text())) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));
  const key = () => ({ 'Idempotency-Key': crypto.randomUUID() });

  // The owner signs in with the old app's password (scrypt, rehashed on the way in).
  await page.goto('/login');
  await page.getByLabel('Email').fill(h.owner.email);
  await page.getByLabel('Password').fill(h.owner.password);
  await page.getByRole('button', { name: /^Sign in( with password)?$/ }).click();
  await expect(page.getByRole('heading', { name: 'Example Household' })).toBeVisible();
  await expect(page.getByText('Avery').first()).toBeVisible();
  const adult = page.request;

  // Avery's iPad, paired in the old app: its signature is accepted.
  const api: APIRequestContext = await playwright.request.newContext({ baseURL: test.info().project.use.baseURL });
  const ipad = new FakeDevice(api, { pem: h.averyIpadKey, id: h.averyIpad });
  const me = await ipad.signed('GET', '/api/device');
  expect(me.status(), await me.text()).toBe(200);

  // Banked iPad time came across as one balance (20 min granted, 5 used): she spends some.
  const banked = async () => {
    const r = await adult.get(`/api/children/${h.avery}/screen-time`);
    expect(r.status()).toBe(200);
    return ((await r.json()) as { bankedSeconds: number }).bankedSeconds;
  };
  expect(await banked()).toBe(900);
  const spend = await ipad.signed('POST', `/api/children/${h.avery}/screen-time/spend`, JSON.stringify({ minutes: 5 }));
  expect(spend.status(), await spend.text()).toBe(200);
  expect(await banked()).toBe(600);

  // Her TV time (30 min bought, 10 watched) too: she watches the Den TV, and the router lets
  // its MAC online only while she does.
  const tvLeft = async () => {
    const r = await ipad.signed('GET', `/api/children/${h.avery}/tv`);
    expect(r.status()).toBe(200);
    return ((await r.json()) as { secondsRemaining: number }).secondsRemaining;
  };
  const routerAllows = async () => {
    const r = await api.get('/api/tv/allowances', { headers: { Authorization: `Bearer ${h.routerToken}` } });
    expect(r.status(), await r.text()).toBe(200);
    return ((await r.json()) as { allowances: { mac: string }[] }).allowances.some((a) => a.mac.toLowerCase() === h.denTvMac);
  };
  expect(await tvLeft()).toBe(1200);
  expect(await routerAllows()).toBe(false);
  const ask = await adult.post(`/api/tvs/${h.denTv}/ask`, { headers: key() });
  expect(ask.status(), await ask.text()).toBe(200);
  const watch = await ipad.signed('POST', `/api/children/${h.avery}/tv/watch`, JSON.stringify({ tvId: h.denTv }));
  expect(watch.status(), await watch.text()).toBe(200);
  const started = Date.parse(((await watch.json()) as { startedAt: string }).startedAt);
  expect(await routerAllows()).toBe(true);
  // Charged by the whole second: watch for more than one.
  await expect.poll(() => Date.now() - started).toBeGreaterThan(1_500);
  const done = await ipad.signed('POST', `/api/tvs/${h.denTv}/done`);
  expect(done.status(), await done.text()).toBe(200);
  expect(await routerAllows()).toBe(false);
  const left = await tvLeft();
  expect(left).toBeLessThan(1200);
  expect(left).toBeGreaterThan(1100);

  // Her history: the chore an adult approved in the old app, and the rewards she had.
  const history = await adult.get(`/api/children/${h.avery}/history`);
  expect(history.status()).toBe(200);
  const entries = ((await history.json()) as { entries: { kind: string; title: string }[] }).entries;
  for (const [kind, title] of [['earned', 'Feed the cat'], ['redeemed', 'TV time'], ['redeemed', 'Ice cream']]) {
    expect(entries.some((e) => e.kind === kind && e.title === title), `${kind} ${title} in ${JSON.stringify(entries)}`).toBe(true);
  }
  await page.goto(`/children/${h.avery}/points`);
  await expect(page.getByText('Feed the cat').first()).toBeVisible();

  // The imported TV time's redemption is given, and has no time of its own to refund: no cancel.
  const given = await adult.get('/api/redemptions?status=fulfilled');
  const rows = ((await given.json()) as { redemptions: { id: string; cancellable: boolean }[] }).redemptions;
  expect(rows.find((r) => r.id === h.tvRedemption)?.cancellable).toBe(false);
  await page.goto('/redemptions');
  const tvRow = page.getByRole('listitem').filter({ hasText: 'TV time' }).filter({ hasText: 'Avery' }).first();
  await expect(tvRow).toBeVisible();
  await expect(tvRow.getByRole('button', { name: /^Cancel/ })).toHaveCount(0);

  await api.dispose();
  expect(problems).toEqual([]);
});
