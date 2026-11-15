import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, ConfirmDialog, EmptyState, Field, Input, List, ListRow, Notice, PageHeader, pointsLabel } from '@app/design';

import { $api, errorMessage, idempotencyKey, useAttempts } from '../api/client';

// Manage: what the children can spend points on. Removing one keeps its history.
export function RewardsPage() {
  const queryClient = useQueryClient();
  const rewards = $api.useQuery('get', '/rewards');
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/rewards'] });
  const create = $api.useMutation('post', '/rewards', { onSuccess: refresh });
  const archive = $api.useMutation('delete', '/rewards/{rewardId}', { onSuccess: refresh });
  const [title, setTitle] = useState('');
  const [cost, setCost] = useState('5');
  const [kind, setKind] = useState<'item' | 'screen_time'>('item');
  const [minutes, setMinutes] = useState('20');
  const [removing, setRemoving] = useState<{ id: string; title: string } | null>(null);
  const attempts = useAttempts();

  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    const body = { title, cost: Number(cost), kind, ...(kind === 'screen_time' ? { minutes: Number(minutes) } : {}) };
    const attempt = JSON.stringify(body);
    create.mutate({ params: attempts.params(attempt), body }, { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setTitle(''); } });
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Rewards" description="What the children can spend their points on." />
      <form onSubmit={submit} className="flex flex-col gap-4" aria-label="Add a reward">
        <Input label="Reward" value={title} onChange={(e) => { setTitle(e.target.value); }} placeholder="Choose a game" maxLength={80} required />
        <Input label="Cost in points" type="number" inputMode="numeric" min={1} max={100000} value={cost} onChange={(e) => { setCost(e.target.value); }} required />
        <Field label="Kind" htmlFor="reward-kind">
          <select id="reward-kind" value={kind} onChange={(e) => { setKind(e.target.value === 'screen_time' ? 'screen_time' : 'item'); }}>
            <option value="item">Something to do or have</option>
            <option value="screen_time">Screen time</option>
          </select>
        </Field>
        {kind === 'screen_time' && (
          <Input label="Minutes" type="number" inputMode="numeric" min={1} max={1440} value={minutes} onChange={(e) => { setMinutes(e.target.value); }} required />
        )}
        {create.error && <Notice tone="error">{errorMessage(create.error)}</Notice>}
        <div>
          <ButtonPrimary type="submit" disabled={create.isPending}>Add reward</ButtonPrimary>
        </div>
      </form>
      {rewards.data?.rewards.length ? (
        <List aria-label="Rewards">
          {rewards.data.rewards.map((r) => (
            <ListRow
              key={r.id}
              title={r.title}
              meta={[pointsLabel(r.cost), r.minutes === undefined ? null : `${String(r.minutes)} minutes`].filter(Boolean).join(' · ')}
              trailing={
                <Button aria-label={`Remove ${r.title}`} onClick={() => { setRemoving(r); }}>
                  Remove
                </Button>
              }
            />
          ))}
        </List>
      ) : (
        rewards.isSuccess && <EmptyState title="No rewards yet" description="Add something worth saving points for." variant="surface" />
      )}
      <ConfirmDialog
        open={removing !== null}
        title={removing ? `Remove ${removing.title}?` : ''}
        description="Children can't choose it any more. Requests already waiting in the inbox stay."
        confirmLabel="Remove"
        tone="danger"
        pending={archive.isPending}
        error={archive.error ? errorMessage(archive.error) : undefined}
        onCancel={() => { setRemoving(null); archive.reset(); }}
        onConfirm={() => {
          if (!removing) return;
          archive.mutate({ params: { ...idempotencyKey(), path: { rewardId: removing.id } } }, { onSuccess: () => { setRemoving(null); } });
        }}
      />
    </section>
  );
}
