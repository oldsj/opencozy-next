import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { MonitorSmartphone } from 'lucide-react';

import { Button, ButtonPrimary, Checkbox, CheckboxGroup, CodeInput, ConfirmDialog, EmptyState, Input, List, ListRow, Notice, PageHeader, Section, Sheet } from '@app/design';

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
  const [pairing, setPairing] = useState(false);
  const [unpairing, setUnpairing] = useState<Display | null>(null);
  const attempts = useAttempts();
  const reauth = useReauth();

  const pair = (body: { code: string; name: string; scopes: DisplayScope[] }) => {
    const attempt = JSON.stringify(body);
    create.mutate(
      { params: attempts.params(attempt), body },
      {
        onSettled: attempts.settleIfAnswered(attempt),
        onSuccess: () => { setCode(''); setName(''); setPairing(false); },
        onError: (error) => { reauth.ask(error, () => { pair(body); }); },
      },
    );
  };
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    pair({ code, name, scopes });
  };

  const openPair = () => { create.reset(); setPairing(true); };

  return (
    <>
      <PageHeader
        title="Displays"
        description="Wall displays show the family’s day. Pair one with the code it shows."
        actions={<ButtonPrimary onClick={openPair}>Pair a display</ButtonPrimary>}
      />
      {displays.isError && <Notice tone="error">{errorMessage(displays.error)}</Notice>}
      {displays.data?.length ? (
        <Section title="Paired displays" count={displays.data.length}>
          <List aria-label="Paired displays">
            {displays.data.map((d) => (
              <ListRow
                key={d.id}
                leading={<MonitorSmartphone aria-hidden="true" size={22} />}
                title={d.name}
                meta={`Shows ${d.scopes.map((s) => scopeLabels[s]).join(' · ')}`}
                trailing={
                  <Button variant="quiet" aria-label={`Unpair ${d.name}`} onClick={() => { setUnpairing(d); }}>
                    Unpair…
                  </Button>
                }
              />
            ))}
          </List>
        </Section>
      ) : (
        displays.isSuccess && (
          <EmptyState
            variant="surface"
            icon={MonitorSmartphone}
            title="No displays yet"
            description="Open this app’s /display page on the wall display: it shows a code to pair it with."
            action={<ButtonPrimary onClick={openPair}>Pair a display</ButtonPrimary>}
          />
        )
      )}
      <Sheet
        open={pairing}
        onClose={() => { setPairing(false); }}
        title="Pair a display"
        description="Open /display on the wall display; it shows a six-letter code."
        footer={
          <>
            <Button onClick={() => { setPairing(false); }}>Cancel</Button>
            <ButtonPrimary type="submit" form="display-pair" disabled={create.isPending || scopes.length === 0}>Pair display</ButtonPrimary>
          </>
        }
      >
        <form id="display-pair" onSubmit={submit} className="grid gap-5" aria-label="Pair a display">
          {create.error && !reauth.needsPassword(create.error) && <Notice tone="error">{errorMessage(create.error)}</Notice>}
          <CodeInput label="Code on the display" value={code} onChange={(e) => { setCode(e.target.value.toUpperCase()); }} required />
          <Input label="Name" value={name} onChange={(e) => { setName(e.target.value); }} placeholder="Kitchen" maxLength={80} required />
          <CheckboxGroup legend="Shows" description="Today is on every display.">
            {(Object.keys(scopeLabels) as DisplayScope[]).map((s) => (
              <Checkbox
                key={s}
                label={scopeLabels[s]}
                // Today is always on: every display shows it.
                checked={s === 'today' || scopes.includes(s)}
                disabled={s === 'today'}
                onChange={(e) => { setScopes(e.target.checked ? [...scopes, s] : scopes.filter((x) => x !== s)); }}
              />
            ))}
          </CheckboxGroup>
        </form>
      </Sheet>
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
    </>
  );
}
