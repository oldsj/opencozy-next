import { useQueryClient } from '@tanstack/react-query';
import { useState, type ReactNode, type SyntheticEvent } from 'react';

import { ButtonPrimary, Input, Notice, PageHeader, Panel } from '@app/design';

import { $api, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';
import { EditError } from './edit-error';

type Household = components['schemas']['Household'];

// IANA Area/City zones (and UTC): the only names the server takes.
const timeZones = ['UTC', ...Intl.supportedValuesOf('timeZone').filter((z) => z.includes('/'))];

// Manage: the household's name and the time zone its day follows.
export function HouseholdPage() {
  const queryClient = useQueryClient();
  const household = $api.useQuery('get', '/household');
  // A new zone moves the household day: everything shown may change. A stale save refetches too.
  const refresh = () => queryClient.invalidateQueries();
  const save = $api.useMutation('patch', '/household', { onSuccess: refresh, onError: refresh });
  const attempts = useAttempts();
  const [saved, setSaved] = useState(false);

  const submit = (h: Household, body: { name: string; timeZone: string }) => {
    const attempt = `household:${JSON.stringify(body)}:${String(h.version)}`;
    setSaved(false);
    save.mutate(
      { params: attempts.params(attempt), body: { ...body, expectedVersion: h.version } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setSaved(true); } },
    );
  };

  return (
    <>
      <PageHeader title="Household" description="The name everyone sees, and the time zone the household’s day follows." />
      {household.data && (
        <HouseholdForm key={household.data.version} household={household.data} pending={save.isPending} onSave={(body) => { submit(household.data, body); }}>
          <EditError error={save.error} />
          {saved && !save.error && <Notice tone="success">Saved.</Notice>}
        </HouseholdForm>
      )}
    </>
  );
}

function HouseholdForm({ household, pending, onSave, children }: { household: Household; pending: boolean; onSave: (body: { name: string; timeZone: string }) => void; children: ReactNode }) {
  const [name, setName] = useState(household.name);
  const [timeZone, setTimeZone] = useState(household.timeZone);
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    onSave({ name, timeZone });
  };
  return (
    <Panel className="max-w-xl">
    <form onSubmit={submit} className="grid gap-6" aria-label="Household settings">
      {children}
      <Input label="Name" value={name} onChange={(e) => { setName(e.target.value); }} maxLength={80} hint="On the wall display and the children’s screens." required />
      <Input
        label="Time zone"
        list="time-zones"
        value={timeZone}
        onChange={(e) => { setTimeZone(e.target.value); }}
        hint="The household day starts at midnight here: chores reset at midnight in this zone."
        required
      />
      <datalist id="time-zones">
        {timeZones.map((z) => (
          <option key={z} value={z} />
        ))}
      </datalist>
      <div>
        <ButtonPrimary type="submit" disabled={pending}>Save</ButtonPrimary>
      </div>
    </form>
    </Panel>
  );
}
