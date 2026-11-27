import { useQueryClient } from '@tanstack/react-query';
import { ClipboardCheck, Users } from 'lucide-react';
import { useState, type ReactNode, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, ConfirmDialog, DayPicker, EmptyState, Field, Input, List, ListRow, Notice, PageHeader, pointsLabel, ProfileAvatar, Section, SegmentedControl, Sheet } from '@app/design';

import { $api, errorMessage, idempotencyKey, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';
import { RouterButtonLink } from '../link';
import { EditError } from './edit-error';

type Chore = components['schemas']['Chore'];
type Child = components['schemas']['Child'];

const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function schedule(days: number[]): string {
  return days.length === 7 ? 'Every day' : days.map((d) => weekdays[d]).join(' · ');
}

// Manage: the chores each child has. A chore done on the wall display waits for
// an adult's approval before its points land.
export function ChoresPage() {
  const queryClient = useQueryClient();
  const children = $api.useQuery('get', '/children');
  const chores = $api.useQuery('get', '/chores');
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['get', '/children'] });
    await queryClient.invalidateQueries({ queryKey: ['get', '/chores'] });
  };
  const addChore = $api.useMutation('post', '/chores', { onSuccess: refresh });
  const archive = $api.useMutation('delete', '/chores/{choreId}', { onSuccess: refresh });
  // A stale save refetches, and the form below starts again from the newer version.
  const edit = $api.useMutation('put', '/chores/{choreId}', { onSuccess: refresh, onError: refresh });
  const [editingId, setEditingId] = useState<string | null>(null);

  const [adding, setAdding] = useState(false);
  const [childId, setChildId] = useState('');
  const [title, setTitle] = useState('');
  const [points, setPoints] = useState('1');
  const [removing, setRemoving] = useState<{ id: string; title: string } | null>(null);
  const attempts = useAttempts();

  const live = children.data?.items.filter((c) => !c.archived) ?? [];
  const chosen = childId || live[0]?.id || '';
  const nameOf = (id: string) => children.data?.items.find((c) => c.id === id)?.name ?? '';
  const editing = chores.data?.find((c) => c.id === editingId);
  const closeEdit = () => { setEditingId(null); edit.reset(); };
  const save = (chore: Chore, body: ChoreBody) => {
    // The key covers the whole request, version included: after a lost answer and a refetch, the
    // same fields at the newer version are a new request, not a reuse of the old key.
    const attempt = `edit:${chore.id}:${String(chore.version)}:${JSON.stringify(body)}`;
    edit.mutate(
      { params: { ...attempts.params(attempt), path: { choreId: chore.id } }, body: { ...body, expectedVersion: chore.version } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setEditingId(null); } },
    );
  };

  const submitChore = (e: SyntheticEvent) => {
    e.preventDefault();
    const body = { childId: chosen, title, points: Number(points) };
    const attempt = `chore:${JSON.stringify(body)}`;
    addChore.mutate({ params: attempts.params(attempt), body }, { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setTitle(''); setAdding(false); } });
  };
  const openAdd = () => { addChore.reset(); setAdding(true); };
  const list = chores.data ?? [];
  // One section per child, in the children's order; chores of an archived child last.
  const owners = [...live, ...(children.data?.items.filter((c) => c.archived && list.some((ch) => ch.childId === c.id)) ?? [])];

  return (
    <>
      <PageHeader
        title="Chores"
        description="What each child can do for points, shown on the wall display on their days."
        actions={live.length > 0 && <ButtonPrimary onClick={openAdd}>Add chore</ButtonPrimary>}
      />
      {(chores.isError || children.isError) && <Notice tone="error">{errorMessage(chores.error ?? children.error)}</Notice>}
      {children.isSuccess && live.length === 0 ? (
        <EmptyState
          variant="surface"
          icon={Users}
          title="No children yet"
          description="Add a child first, then give them chores."
          action={<RouterButtonLink to="/children" variant="primary">Add a child</RouterButtonLink>}
        />
      ) : chores.isSuccess && list.length === 0 ? (
        <EmptyState variant="surface" icon={ClipboardCheck} title="No chores yet" description="Give each child a chore or two; ticking one off on the wall earns its points once you approve it." action={<ButtonPrimary onClick={openAdd}>Add chore</ButtonPrimary>} />
      ) : (
        owners.map((kid) => {
          const mine = list.filter((c) => c.childId === kid.id);
          if (!mine.length) return null;
          return (
            <Section key={kid.id} title={kid.archived ? `${kid.name} (archived)` : kid.name} count={mine.length}>
              <List aria-label={`${kid.name}’s chores`}>
                {mine.map((c) => (
                  <ListRow
                    key={c.id}
                    stackOnNarrow
                    leading={<ProfileAvatar name={kid.name} color={kid.color} size="small" aria-hidden="true" />}
                    title={c.title}
                    meta={`${pointsLabel(c.points)} · ${schedule(c.days)}`}
                    trailing={
                      <>
                        <Button variant="quiet" aria-label={`Edit ${c.title}`} onClick={() => { edit.reset(); setEditingId(c.id); }}>
                          Edit
                        </Button>
                        <Button variant="quiet" aria-label={`Remove ${c.title}`} onClick={() => { setRemoving(c); }}>
                          Remove…
                        </Button>
                      </>
                    }
                  />
                ))}
              </List>
            </Section>
          );
        })
      )}
      <Sheet
        open={adding}
        onClose={() => { setAdding(false); }}
        title="Add a chore"
        description="It shows on the child’s list every day; choose the days after adding it."
        footer={
          <>
            <Button onClick={() => { setAdding(false); }}>Cancel</Button>
            <ButtonPrimary type="submit" form="chore-add" disabled={addChore.isPending}>Add chore</ButtonPrimary>
          </>
        }
      >
        <form id="chore-add" onSubmit={submitChore} className="grid gap-5" aria-label="Add a chore">
          {addChore.error && <Notice tone="error">{errorMessage(addChore.error)}</Notice>}
          <WhoseControl label="For" value={chosen} kids={live} onChange={setChildId} />
          <Input label="Chore" value={title} onChange={(e) => { setTitle(e.target.value); }} placeholder="Feed the cat" maxLength={80} required />
          <Input label="Points" type="number" inputMode="numeric" min={0} max={1000} value={points} onChange={(e) => { setPoints(e.target.value); }} required />
        </form>
      </Sheet>
      <Sheet
        open={editing !== undefined}
        onClose={closeEdit}
        title={editing ? `Edit ${editing.title}` : ''}
        description="Changes apply from today. Points already earned stay."
        footer={
          <>
            <Button onClick={closeEdit}>Cancel</Button>
            <ButtonPrimary type="submit" form="chore-edit" disabled={edit.isPending}>Save</ButtonPrimary>
          </>
        }
      >
        {editing && (
          <ChoreForm key={`${editing.id}:${String(editing.version)}`} chore={editing} kids={live} nameOf={nameOf} onSave={(body) => { save(editing, body); }}>
            <EditError error={edit.error} />
          </ChoreForm>
        )}
      </Sheet>
      <ConfirmDialog
        open={removing !== null}
        title={removing ? `Remove ${removing.title}?` : ''}
        description="It leaves the wall display from today. Points already earned stay."
        confirmLabel="Remove"
        tone="danger"
        pending={archive.isPending}
        error={archive.error ? errorMessage(archive.error) : undefined}
        onCancel={() => { setRemoving(null); archive.reset(); }}
        onConfirm={() => {
          if (!removing) return;
          archive.mutate({ params: { ...idempotencyKey(), path: { choreId: removing.id } } }, { onSuccess: () => { setRemoving(null); } });
        }}
      />
    </>
  );
}

type ChoreBody = { childId: string; title: string; points: number; days: number[] };

/** The edit form, started from one version of the chore (keyed by it, so a newer one starts again). */
function ChoreForm({ chore, children, kids, nameOf, onSave }: { chore: Chore; children: ReactNode; kids: Child[]; nameOf: (id: string) => string; onSave: (body: ChoreBody) => void }) {
  const [title, setTitle] = useState(chore.title);
  const [childId, setChildId] = useState(chore.childId);
  const [points, setPoints] = useState(String(chore.points));
  const [days, setDays] = useState<number[]>(chore.days);
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    onSave({ childId, title, points: Number(points), days });
  };
  return (
    <form id="chore-edit" onSubmit={submit} className="grid gap-5">
      {children}
      <Input label="Chore" value={title} onChange={(e) => { setTitle(e.target.value); }} maxLength={80} required />
      <WhoseControl label="Whose chore" value={childId} kids={kids} onChange={setChildId} />
      {childId !== chore.childId && (
        <p className="m-0 text-muted-foreground">{`Moving it to ${nameOf(childId)}: it leaves ${nameOf(chore.childId)}’s list today and shows on ${nameOf(childId)}’s.`}</p>
      )}
      <Input label="Points" type="number" inputMode="numeric" min={0} max={1000} value={points} onChange={(e) => { setPoints(e.target.value); }} required />
      <DayPicker label={`Days for ${chore.title}`} value={days} onChange={setDays} />
    </form>
  );
}

/** Which child: a segmented control for up to five children, a menu beyond that. */
export function WhoseControl({ label, value, kids, onChange }: { label: string; value: string; kids: Child[]; onChange: (id: string) => void }) {
  if (kids.length <= 5) {
    return <SegmentedControl label={label} value={value} onChange={onChange} options={kids.map((k) => ({ value: k.id, label: k.name }))} />;
  }
  return (
    <Field label={label} htmlFor="whose">
      <select id="whose" value={value} onChange={(e) => { onChange(e.target.value); }}>
        {kids.map((k) => (
          <option key={k.id} value={k.id}>{k.name}</option>
        ))}
      </select>
    </Field>
  );
}
