import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, CodeInput, ConfirmDialog, EmptyState, Input, List, ListRow, Notice, PageHeader } from '@app/design';

import { $api, errorMessage, idempotencyKey, useAttempts, type Display, type DisplayScope } from '../api/client';
import { useReauth } from './reauth';

const scopeLabels: Record<DisplayScope, string> = {
  today: 'Today',
  tv: 'TV',
  calendar: 'Calendar',
  meals: 'Meals',
  dashboard: 'Dashboard',
};

// Manage: pair a wall display by its code, see the paired ones, unpair one.
export function DisplaysPage() {
  const queryClient = useQueryClient();
  const displays = $api.useQuery('get', '/displays');
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/displays'] });
  const create = $api.useMutation('post', '/displays', { onSuccess: refresh });
  const revoke = $api.useMutation('delete', '/displays/{displayId}', { onSuccess: refresh });
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<DisplayScope[]>(['today']);
  const [unpairing, setUnpairing] = useState<Display | null>(null);
  const attempts = useAttempts();
  const reauth = useReauth();

  const pair = (body: { code: string; name: string; scopes: DisplayScope[] }) => {
    const attempt = JSON.stringify(body);
    create.mutate(
      { params: attempts.params(attempt), body },
      {
        onSettled: attempts.settleIfAnswered(attempt),
        onSuccess: () => { setCode(''); setName(''); },
        onError: (error) => { reauth.ask(error, () => { pair(body); }); },
      },
    );
  };
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    pair({ code, name, scopes });
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Displays" description="Wall displays show the family's day. Pair one with the code it shows." />
      <form onSubmit={submit} className="flex flex-col gap-4" aria-label="Pair a display">
        <CodeInput label="Code on the display" value={code} onChange={(e) => { setCode(e.target.value.toUpperCase()); }} required />
        <Input label="Name" value={name} onChange={(e) => { setName(e.target.value); }} placeholder="Kitchen" maxLength={80} required />
        <fieldset className="flex flex-wrap gap-4">
          <legend className="text-section mb-2">Shows</legend>
          {(Object.keys(scopeLabels) as DisplayScope[]).map((s) => (
            <label key={s} className="flex items-center gap-2">
              <input
                type="checkbox"
                // Today is always on: every display shows it.
                checked={s === 'today' || scopes.includes(s)}
                disabled={s === 'today'}
                onChange={(e) => { setScopes(e.target.checked ? [...scopes, s] : scopes.filter((x) => x !== s)); }}
              />
              {scopeLabels[s]}
            </label>
          ))}
        </fieldset>
        {create.error && !reauth.needsPassword(create.error) && <Notice tone="error">{errorMessage(create.error)}</Notice>}
        <div>
          <ButtonPrimary type="submit" disabled={create.isPending || scopes.length === 0}>Pair display</ButtonPrimary>
        </div>
      </form>
      {displays.data?.length ? (
        <List aria-label="Paired displays">
          {displays.data.map((d) => (
            <ListRow
              key={d.id}
              title={d.name}
              meta={d.scopes.map((s) => scopeLabels[s]).join(' · ')}
              trailing={
                <Button aria-label={`Unpair ${d.name}`} onClick={() => { setUnpairing(d); }}>
                  Unpair
                </Button>
              }
            />
          ))}
        </List>
      ) : (
        displays.isSuccess && <EmptyState title="No displays yet" description="Open this app's /display page on the wall display to get a code." variant="surface" />
      )}
      <ConfirmDialog
        open={unpairing !== null}
        title={unpairing ? `Unpair ${unpairing.name}?` : ''}
        description="It stops showing the family's day at once. To use it again, pair it with a new code."
        confirmLabel="Unpair"
        tone="danger"
        pending={revoke.isPending}
        error={revoke.error ? errorMessage(revoke.error) : undefined}
        onCancel={() => { setUnpairing(null); revoke.reset(); }}
        onConfirm={() => {
          if (!unpairing) return;
          revoke.mutate({ params: { ...idempotencyKey(), path: { displayId: unpairing.id } } }, { onSuccess: () => { setUnpairing(null); } });
        }}
      />
      {reauth.dialog}
    </section>
  );
}
