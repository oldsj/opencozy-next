import { createHash, createSign, generateKeyPairSync, randomBytes } from 'node:crypto';
import { createServer, type Server } from 'node:http';

import { owner } from './owner';

// A fake OpenID Connect provider on 127.0.0.1 for the single sign-on test. Only an e2e build
// (go build -tags e2e) accepts an http issuer there, and only its OIDC client may reach it. It
// signs anyone in as the e2e owner, with a verified email.
export const oidcPort = Number(process.env.E2E_OIDC_PORT ?? String(Number(process.env.E2E_PORT ?? '18080') + 1000));
export const oidcIssuer = `http://127.0.0.1:${String(oidcPort)}`;
export const oidcClientID = 'opencozy-e2e';
// A fixture, not a secret: it guards nothing but this fake provider.
export const oidcClientSecret = 'e2e-fixture-client-secret';
export const oidcName = 'Example ID';

const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
const kid = 'e2e';
const b64url = (b: Buffer | string) => Buffer.from(b).toString('base64url');

function idToken(nonce: string): string {
  const now = Math.floor(Date.now() / 1000);
  const header = b64url(JSON.stringify({ alg: 'RS256', typ: 'JWT', kid }));
  const claims = b64url(JSON.stringify({
    iss: oidcIssuer, aud: oidcClientID, sub: 'e2e-subject-1', iat: now, exp: now + 300, nonce,
    email: owner.email, email_verified: true,
  }));
  const signature = createSign('RSA-SHA256').update(`${header}.${claims}`).sign(privateKey);
  return `${header}.${claims}.${b64url(signature)}`;
}

// Codes handed out by /authorize, waiting for /token.
const codes = new Map<string, { nonce: string; challenge: string; redirect: string }>();

function readBody(req: import('node:http').IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let body = '';
    req.on('data', (c: Buffer) => { body += c.toString(); });
    req.on('end', () => { resolve(body); });
    req.on('error', reject);
  });
}

export function serveOIDC(): Promise<Server> {
  const server = createServer((req, res) => {
    void (async () => {
      const url = new URL(req.url ?? '/', oidcIssuer);
      const json = (status: number, body: unknown) => res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(body));
      switch (url.pathname) {
        case '/.well-known/openid-configuration':
          json(200, {
            issuer: oidcIssuer, authorization_endpoint: `${oidcIssuer}/authorize`, token_endpoint: `${oidcIssuer}/token`,
            jwks_uri: `${oidcIssuer}/jwks`, response_types_supported: ['code'], subject_types_supported: ['public'],
            id_token_signing_alg_values_supported: ['RS256'], code_challenge_methods_supported: ['S256'],
          });
          return;
        case '/jwks':
          json(200, { keys: [{ ...publicKey.export({ format: 'jwk' }), kid, use: 'sig', alg: 'RS256' }] });
          return;
        case '/authorize': {
          // Signs in straight away: no provider login page to drive.
          const q = url.searchParams;
          const redirect = q.get('redirect_uri') ?? '';
          if (q.get('client_id') !== oidcClientID || q.get('code_challenge_method') !== 'S256' || !redirect) {
            res.writeHead(400).end();
            return;
          }
          const code = randomBytes(16).toString('hex');
          codes.set(code, { nonce: q.get('nonce') ?? '', challenge: q.get('code_challenge') ?? '', redirect });
          const back = new URL(redirect);
          back.searchParams.set('code', code);
          back.searchParams.set('state', q.get('state') ?? '');
          back.searchParams.set('iss', oidcIssuer);
          res.writeHead(302, { Location: back.toString() }).end();
          return;
        }
        case '/token': {
          const form = new URLSearchParams(await readBody(req));
          const basic = /^Basic (.+)$/.exec(req.headers.authorization ?? '')?.[1];
          const [id, secret] = basic
            ? Buffer.from(basic, 'base64').toString().split(':').map(decodeURIComponent)
            : [form.get('client_id'), form.get('client_secret')];
          const grant = codes.get(form.get('code') ?? '');
          codes.delete(form.get('code') ?? '');
          const verifier = form.get('code_verifier') ?? '';
          if (id !== oidcClientID || secret !== oidcClientSecret || !grant || grant.redirect !== form.get('redirect_uri')
            || b64url(createHash('sha256').update(verifier).digest()) !== grant.challenge) {
            json(400, { error: 'invalid_grant' });
            return;
          }
          json(200, { access_token: randomBytes(16).toString('hex'), token_type: 'Bearer', expires_in: 300, id_token: idToken(grant.nonce) });
          return;
        }
      }
      res.writeHead(404).end();
    })();
  });
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(oidcPort, '127.0.0.1', () => { resolve(server); });
  });
}
