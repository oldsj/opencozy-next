import { createHash, generateKeyPairSync, sign, type KeyObject } from 'node:crypto';

import { expect, type APIRequestContext } from '@playwright/test';

// The kid app, played in Node: a P-256 key, the pairing proof and signed requests, exactly as
// the iPad signs them (raw r||s, base64url).
export class FakeDevice {
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
  /** A signed request; a POST gets a fresh Idempotency-Key (commands need one). */
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
    return method === 'GET' ? this.api.get(path, { headers }) : this.api.post(path, { headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() }, data: body });
  }
}
