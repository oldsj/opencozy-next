// The imported household (internal/importer/testdata/old_seed.sql, adjusted by
// scripts/import-rehearsal/journey.sql). Fictional, made for the fixture: these open nothing but
// the rehearsal's throwaway database.
export const household = {
  owner: { email: 'owner@example.test', password: 'fixture-owner-password' },
  avery: '0190a000-0000-7000-8000-0000000000c1',
  averyIpad: '0190a000-0000-7000-8000-0000000000e1',
  // journey.sql gives Avery's iPad this key's public half.
  averyIpadKey: `-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgL3XacfDewJotk/vv
cO8GFOnusiOR6tOE5nzsEPaZcfuhRANCAAQjc7clUnYayXJ2xHT2lrRQo2hed3po
/j5s4IGQOQBJFySvTlmoeOPO9Pq2rVpF51+GvEBW9cjXZVnwpawSetVf
-----END PRIVATE KEY-----
`,
  denTv: '0190a000-0000-7000-8000-0000000009a1',
  denTvMac: '02:00:5e:10:00:01',
  tvRedemption: '0190a000-0000-7000-8000-0000000009f2',
  routerToken: 'ocz__koCd_CivNhz2YxuA1Boq99DgqwxWJ9ULvrZgF5RUd0',
};
