import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState, type ReactNode, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, ConfirmDialog, EmptyState, Field, Input, List, ListRow, Notice, PageHeader, pointsLabel, SegmentedControl, Sheet } from '@app/design';

import { $api, errorMessage, idempotencyKey, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';
import { EditError } from './edit-error';

type Reward = components['schemas']['Reward'];
type RewardBody = { title: string; cost: number; kind: 'item' | 'screen_time'; minutes?: number; available: boolean };

// Manage: what the children can spend points on. Removing one keeps its history.
export function RewardsPage() {
  const queryClient = useQueryClient();
  const rewards = $api.useQuery('get', '/rewards');
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/rewards'] });
  const create = $api.useMutation('post', '/rewards', { onSuccess: refresh });
  const archive = $api.useMutation('delete', '/rewards/{rewardId}', { onSuccess: refresh });
  // A stale save refetches, and the form starts again from the newer version.
  const edit = $api.useMutation('put', '/rewards/{rewardId}', { onSuccess: refresh, onError: refresh });
  const [editingId, setEditingId] = useState<string | null>(null);
  const [title, setTitle] = useState('');
  const [cost, setCost] = useState('5');
  const [kind, setKind] = useState<'item' | 'screen_time'>('item');
  const [minutes, setMinutes] = useState('20');
  const [removing, setRemoving] = useState<{ id: string; title: string } | null>(null);
  const attempts = useAttempts();

  const editing = rewards.data?.rewards.find((r) => r.id === editingId);
  const closeEdit = () => { setEditingId(null); edit.reset(); };
  const save = (reward: Reward, body: RewardBody) => {
    // The key covers the whole request, version included: after a lost answer and a refetch, the
    // same fields at the newer version are a new request, not a reuse of the old key.
    const attempt = `edit:${reward.id}:${String(reward.version)}:${JSON.stringify(body)}`;
    edit.mutate(
      { params: { ...attempts.params(attempt), path: { rewardId: reward.id } }, body: { ...body, expectedVersion: reward.version } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setEditingId(null); } },
    );
  };

  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    const body = { title, cost: Number(cost), kind, ...(kind === 'screen_time' ? { minutes: Number(minutes) } : {}) };
    const attempt = JSON.stringify(body);
    create.mutate({ params: attempts.params(attempt), body }, { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setTitle(''); } });
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Rewards" description="What the children can spend their points on." actions={<Link to="/redemptions">Chosen rewards</Link>} />
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
              muted={!r.available}
              title={r.title}
              meta={[pointsLabel(r.cost), r.minutes === undefined ? null : `${String(r.minutes)} minutes`, r.available ? null : 'hidden from children'].filter(Boolean).join(' · ')}
              trailing={
                <>
                  <Button variant="quiet" aria-label={`Edit ${r.title}`} onClick={() => { edit.reset(); setEditingId(r.id); }}>
                    Edit
                  </Button>
                  <Button aria-label={`Remove ${r.title}`} onClick={() => { setRemoving(r); }}>
                    Remove
                  </Button>
                </>
              }
            />
          ))}
        </List>
      ) : (
        rewards.isSuccess && <EmptyState title="No rewards yet" description="Add something worth saving points for." variant="surface" />
      )}
      <Sheet
        open={editing !== undefined}
        onClose={closeEdit}
        title={editing ? `Edit ${editing.title}` : ''}
        description="Requests already waiting in the inbox keep their cost."
        footer={
          <>
            <Button onClick={closeEdit}>Cancel</Button>
            <ButtonPrimary type="submit" form="reward-edit" disabled={edit.isPending}>Save</ButtonPrimary>
          </>
        }
      >
        {editing && (
          <RewardForm key={`${editing.id}:${String(editing.version)}`} reward={editing} onSave={(body) => { save(editing, body); }}>
            <EditError error={edit.error} />
          </RewardForm>
        )}
      </Sheet>
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

/** The edit form, started from one version of the reward (keyed by it, so a newer one starts again). */
function RewardForm({ reward, children, onSave }: { reward: Reward; children: ReactNode; onSave: (body: RewardBody) => void }) {
  const [title, setTitle] = useState(reward.title);
  const [cost, setCost] = useState(String(reward.cost));
  const [kind, setKind] = useState(reward.kind);
  const [minutes, setMinutes] = useState(String(reward.minutes ?? 20));
  const [available, setAvailable] = useState(reward.available ? 'shown' : 'hidden');
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    onSave({ title, cost: Number(cost), kind, ...(kind === 'screen_time' ? { minutes: Number(minutes) } : {}), available: available === 'shown' });
  };
  return (
    <form id="reward-edit" onSubmit={submit} className="grid gap-5">
      {children}
      <Input label="Reward" value={title} onChange={(e) => { setTitle(e.target.value); }} maxLength={80} required />
      <Input label="Cost in points" type="number" inputMode="numeric" min={1} max={100000} value={cost} onChange={(e) => { setCost(e.target.value); }} required />
      <Field label="Kind" htmlFor="reward-edit-kind">
        <select id="reward-edit-kind" value={kind} onChange={(e) => { setKind(e.target.value === 'screen_time' ? 'screen_time' : 'item'); }}>
          <option value="item">Something to do or have</option>
          <option value="screen_time">Screen time</option>
        </select>
      </Field>
      {kind === 'screen_time' && (
        <Input label="Minutes" type="number" inputMode="numeric" min={1} max={1440} value={minutes} onChange={(e) => { setMinutes(e.target.value); }} required />
      )}
      <SegmentedControl
        label="Children can choose it"
        value={available}
        onChange={setAvailable}
        options={[
          { value: 'shown', label: 'Shown' },
          { value: 'hidden', label: 'Hidden' },
        ]}
      />
    </form>
  );
}
