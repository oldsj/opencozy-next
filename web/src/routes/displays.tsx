import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, CodeInput, EmptyState, Input, List, ListRow, Notice, PageHeader } from '@app/design';

import { $api, errorMessage, idempotencyKey, type DisplayScope } from '../api/client';

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

  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    create.mutate(
      { params: idempotencyKey(), body: { code, name, scopes } },
      { onSuccess: () => { setCode(''); setName(''); } },
    );
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
                checked={scopes.includes(s)}
                onChange={(e) => { setScopes(e.target.checked ? [...scopes, s] : scopes.filter((x) => x !== s)); }}
              />
              {scopeLabels[s]}
            </label>
          ))}
        </fieldset>
        {create.error && <Notice tone="error">{errorMessage(create.error)}</Notice>}
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
                <Button
                  onClick={() => { revoke.mutate({ params: { ...idempotencyKey(), path: { displayId: d.id } } }); }}
                  disabled={revoke.isPending}
                >
                  Unpair
                </Button>
              }
            />
          ))}
        </List>
      ) : (
        displays.isSuccess && <EmptyState title="No displays yet" description="Open this app's /display page on the wall display to get a code." variant="surface" />
      )}
    </section>
  );
}
