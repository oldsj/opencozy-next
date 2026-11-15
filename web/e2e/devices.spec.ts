import { createHash, generateKeyPairSync, sign, type KeyObject } from 'node:crypto';

import { expect, test, type APIRequestContext } from '@playwright/test';

import { owner } from './global-setup';

// The kid app, played in Node: a P-256 key, the pairing proof and signed requests, exactly as
// the iPad signs them (raw r||s, base64url).
class FakeDevice {
  readonly key: KeyObject;
  readonly publicKey: string;
  id = '';
  constructor(private readonly api: APIRequestContext) {
    const { privateKey, publicKey } = generateKeyPairSync('ec', { namedCurve: 'P-256' });
    this.key = privateKey;
    const jwk = publicKey.export({ format: 'jwk' });
    const point = Buffer.concat([Buffer.from([4]), Buffer.from(jwk.x ?? '', 'base64url'), Buffer.from(jwk.y ?? '', 'base64url')]);
    this.publicKey = point.toString('base64');
  }
  sign(message: string) {
    return sign('sha256', Buffer.from(message), { key: this.key, dsaEncoding: 'ieee-p1363' }).toString('base64url');
  }
  async pair(code: string, name: string) {
    const res = await this.api.post('/api/device/pair', {
      data: { code, publicKey: this.publicKey, proof: this.sign(['OPENCOZY-PAIR-V1', code, this.publicKey].join('\n')), platform: 'ios', name },
    });
    expect(res.status()).toBe(201);
    this.id = ((await res.json()) as { device: { id: string } }).device.id;
  }
  async signed(method: 'GET' | 'POST', path: string, body = '') {
    const ts = String(Math.floor(Date.now() / 1000));
    const nonce = createHash('sha256').update(String(Math.random())).digest('base64url').slice(0, 32);
    const bodyHash = createHash('sha256').update(body).digest('hex');
    const headers = {
      'X-OpenCozy-Device': this.id,
      'X-OpenCozy-Timestamp': ts,
      'X-OpenCozy-Nonce': nonce,
      'X-OpenCozy-Signature': this.sign(['OPENCOZY-DEVICE-V1', method, path, ts, nonce, bodyHash, this.id].join('\n')),
      'Content-Type': 'application/json',
    };
    return method === 'GET' ? this.api.get(path, { headers }) : this.api.post(path, { headers, data: body });
  }
}

test('pair a child’s iPad, open the kid view, tick a chore, unpair it', async ({ browser, playwright, baseURL }) => {
  const run = String(Date.now()).slice(-6);
  const child = `Juniper ${run}`;
  const chore = `Water the plants ${run}`;
  const adult = await (await browser.newContext()).newPage();
  await adult.goto('/login');
  await adult.getByLabel('Email').fill(owner.email);
  await adult.getByLabel('Password').fill(owner.password);
  await adult.getByRole('button', { name: 'Sign in' }).click();
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
  await kid.getByRole('link', { name: 'Rewards' }).click();
  await expect(kid.getByRole('heading', { name: 'Rewards' })).toBeVisible();
  await expect(kid.getByText('Who’s choosing?')).toHaveCount(0);

  // The link worked once.
  const again = await (await browser.newContext()).newPage();
  await again.goto(url);
  await expect(again).toHaveURL(/\/device\/expired$/);
  await expect(again.getByText('One moment…')).toBeVisible();

  // Unpairing ends it at once.
  await adult.getByRole('button', { name: `Unpair ${child}’s iPad` }).click();
  await adult.getByRole('alertdialog').getByRole('button', { name: `Unpair ${child}’s iPad` }).click();
  await expect(adult.getByText(`${child}’s iPad`, { exact: true })).toHaveCount(0);
  expect((await ipad.signed('GET', '/api/device')).status()).toBe(401);
  await kid.reload();
  await expect(kid.getByText('Ask an adult to check this iPad')).toBeVisible();
  expect(problems).toEqual([]);
});
