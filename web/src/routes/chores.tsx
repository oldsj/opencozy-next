import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, EmptyState, Field, Input, List, ListRow, Notice, PageHeader } from '@app/design';

import { $api, errorMessage, idempotencyKey } from '../api/client';

const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function schedule(days: number[]): string {
  return days.length === 7 ? 'Every day' : days.map((d) => weekdays[d]).join(' · ');
}

// Manage: the children, and the chores each one has. A chore done on the wall display waits for
// an adult's approval before its points land.
export function ChoresPage() {
  const queryClient = useQueryClient();
  const children = $api.useQuery('get', '/children');
  const chores = $api.useQuery('get', '/chores');
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['get', '/children'] });
    await queryClient.invalidateQueries({ queryKey: ['get', '/chores'] });
  };
  const addChild = $api.useMutation('post', '/children', { onSuccess: refresh });
  const addChore = $api.useMutation('post', '/chores', { onSuccess: refresh });
  const archive = $api.useMutation('delete', '/chores/{choreId}', { onSuccess: refresh });

  const [childName, setChildName] = useState('');
  const [childId, setChildId] = useState('');
  const [title, setTitle] = useState('');
  const [points, setPoints] = useState('1');

  const live = children.data?.items.filter((c) => !c.archived) ?? [];
  const chosen = childId || live[0]?.id || '';
  const nameOf = (id: string) => live.find((c) => c.id === id)?.name ?? '';

  const submitChild = (e: SyntheticEvent) => {
    e.preventDefault();
    addChild.mutate({ params: idempotencyKey(), body: { name: childName } }, { onSuccess: () => { setChildName(''); } });
  };
  const submitChore = (e: SyntheticEvent) => {
    e.preventDefault();
    addChore.mutate(
      { params: idempotencyKey(), body: { childId: chosen, title, points: Number(points) } },
      { onSuccess: () => { setTitle(''); } },
    );
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Chores" description="Each child's chores, shown on the wall display every day." />

      <form onSubmit={submitChild} className="flex flex-col gap-4" aria-label="Add a child">
        <Input label="Child's name" value={childName} onChange={(e) => { setChildName(e.target.value); }} maxLength={40} required />
        {addChild.error && <Notice tone="error">{errorMessage(addChild.error)}</Notice>}
        <div>
          <ButtonPrimary type="submit" disabled={addChild.isPending}>Add child</ButtonPrimary>
        </div>
      </form>

      {live.length > 0 && (
        <form onSubmit={submitChore} className="flex flex-col gap-4" aria-label="Add a chore">
          <Field label="For" htmlFor="chore-child">
            <select id="chore-child" value={chosen} onChange={(e) => { setChildId(e.target.value); }}>
              {live.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </Field>
          <Input label="Chore" value={title} onChange={(e) => { setTitle(e.target.value); }} placeholder="Feed the cat" maxLength={80} required />
          <Input label="Points" type="number" inputMode="numeric" min={0} max={1000} value={points} onChange={(e) => { setPoints(e.target.value); }} required />
          {addChore.error && <Notice tone="error">{errorMessage(addChore.error)}</Notice>}
          <div>
            <ButtonPrimary type="submit" disabled={addChore.isPending}>Add chore</ButtonPrimary>
          </div>
        </form>
      )}

      {chores.data?.length ? (
        <List aria-label="Chores">
          {chores.data.map((c) => (
            <ListRow
              key={c.id}
              title={c.title}
              meta={`${nameOf(c.childId)} · ${String(c.points)} points · ${schedule(c.days)}`}
              trailing={
                <Button
                  onClick={() => { archive.mutate({ params: { ...idempotencyKey(), path: { choreId: c.id } } }); }}
                  disabled={archive.isPending}
                >
                  Remove
                </Button>
              }
            />
          ))}
        </List>
      ) : (
        chores.isSuccess && <EmptyState title="No chores yet" description="Add a child, then give them a chore." variant="surface" />
      )}
    </section>
  );
}
