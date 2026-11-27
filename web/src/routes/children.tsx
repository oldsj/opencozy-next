import { useQueryClient } from '@tanstack/react-query';
import { Users } from 'lucide-react';
import { useState, type ReactNode, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, ConfirmDialog, EmptyState, Input, List, ListRow, Notice, PageHeader, ProfileAvatar, Section, Sheet } from '@app/design';

import { $api, errorMessage, useAttempts, type Child } from '../api/client';
import { RouterButtonLink } from '../link';
import { EditError } from './edit-error';

// Manage: the children. Each has their points; archiving takes a child off every screen and
// keeps their points and history.
export function ChildrenPage() {
  const queryClient = useQueryClient();
  const children = $api.useQuery('get', '/children');
  const refresh = () => queryClient.invalidateQueries();
  const add = $api.useMutation('post', '/children', { onSuccess: refresh });
  // A stale save refetches, so the next try starts from the newer version.
  const update = $api.useMutation('patch', '/children/{childId}', { onSuccess: refresh, onError: refresh });
  const attempts = useAttempts();

  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [renaming, setRenaming] = useState<Child | null>(null);
  const [archiving, setArchiving] = useState<Child | null>(null);

  const all = children.data?.items ?? [];
  const live = all.filter((c) => !c.archived);
  const archived = all.filter((c) => c.archived);

  const submitAdd = (e: SyntheticEvent) => {
    e.preventDefault();
    const body = { name };
    const attempt = `child:${JSON.stringify(body)}`;
    add.mutate(
      { params: attempts.params(attempt), body },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setName(''); setAdding(false); } },
    );
  };
  const patch = (child: Child, body: { name: string; archived: boolean }, done: () => void) => {
    const attempt = `child:${child.id}:${String(child.version)}:${JSON.stringify(body)}`;
    update.mutate(
      { params: { ...attempts.params(attempt), path: { childId: child.id } }, body: { ...body, expectedVersion: child.version } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: done },
    );
  };
  const openAdd = () => { add.reset(); setAdding(true); };

  return (
    <>
      <PageHeader title="Children" description="Who the family board is for. Each child has their own colour, list and points." actions={<ButtonPrimary onClick={openAdd}>Add a child</ButtonPrimary>} />
      {children.isError && <Notice tone="error">{errorMessage(children.error)}</Notice>}
      {update.error && !renaming && !archiving && <Notice tone="error">{errorMessage(update.error)}</Notice>}
      {children.isSuccess && (
        <Section title="Children" count={live.length} hideTitle={archived.length === 0}>
          {live.length ? (
            <List aria-label="Children">
              {live.map((c) => (
                <ListRow
                  key={c.id}
                  stackOnNarrow
                  leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
                  title={c.name}
                  trailing={
                    <>
                      <RouterButtonLink to="/children/$childId/points" params={{ childId: c.id }} variant="quiet" aria-label={`${c.name}’s points`}>
                        Points
                      </RouterButtonLink>
                      <Button variant="quiet" aria-label={`Rename ${c.name}`} onClick={() => { update.reset(); setRenaming(c); }}>
                        Rename
                      </Button>
                      <Button variant="quiet" aria-label={`Archive ${c.name}`} onClick={() => { update.reset(); setArchiving(c); }}>
                        Archive…
                      </Button>
                    </>
                  }
                />
              ))}
            </List>
          ) : (
            <EmptyState
              variant="surface"
              icon={Users}
              title="No children yet"
              description="Add a child to give them chores, routines and rewards."
              action={<ButtonPrimary onClick={openAdd}>Add a child</ButtonPrimary>}
            />
          )}
        </Section>
      )}
      {archived.length > 0 && (
        <Section title="Archived" count={archived.length} description="Off every screen, with their points and history kept.">
          <List aria-label="Archived children">
            {archived.map((c) => (
              <ListRow
                key={c.id}
                muted
                leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
                title={c.name}
                trailing={
                  <Button aria-label={`Restore ${c.name}`} disabled={update.isPending} onClick={() => { patch(c, { name: c.name, archived: false }, () => undefined); }}>
                    Restore
                  </Button>
                }
              />
            ))}
          </List>
        </Section>
      )}

      <Sheet
        open={adding}
        onClose={() => { setAdding(false); }}
        title="Add a child"
        description="They get a colour of their own; their list starts empty."
        footer={
          <>
            <Button onClick={() => { setAdding(false); }}>Cancel</Button>
            <ButtonPrimary type="submit" form="child-add" disabled={add.isPending}>Add child</ButtonPrimary>
          </>
        }
      >
        <form id="child-add" onSubmit={submitAdd} className="grid gap-5" aria-label="Add a child">
          {add.error && <Notice tone="error">{errorMessage(add.error)}</Notice>}
          <Input label="Child's name" value={name} onChange={(e) => { setName(e.target.value); }} maxLength={40} required />
        </form>
      </Sheet>

      <Sheet
        open={renaming !== null}
        onClose={() => { setRenaming(null); }}
        title={renaming ? `Rename ${renaming.name}` : ''}
        footer={
          <>
            <Button onClick={() => { setRenaming(null); }}>Cancel</Button>
            <ButtonPrimary type="submit" form="child-rename" disabled={update.isPending}>Save</ButtonPrimary>
          </>
        }
      >
        {renaming && (
          <RenameForm
            key={`${renaming.id}:${String(renaming.version)}`}
            child={renaming}
            onSave={(newName) => { patch(renaming, { name: newName, archived: false }, () => { setRenaming(null); }); }}
          >
            <EditError error={update.error} />
          </RenameForm>
        )}
      </Sheet>

      <ConfirmDialog
        open={archiving !== null}
        title={archiving ? `Archive ${archiving.name}?` : ''}
        description={
          archiving
            ? `${archiving.name} leaves the wall display, the inbox and every list. Their points and history are kept, and you can restore them any time.`
            : ''
        }
        confirmLabel="Archive"
        tone="danger"
        pending={update.isPending}
        error={update.error ? errorMessage(update.error) : undefined}
        onCancel={() => { setArchiving(null); update.reset(); }}
        onConfirm={() => {
          if (archiving) patch(archiving, { name: archiving.name, archived: true }, () => { setArchiving(null); });
        }}
      />
    </>
  );
}

function RenameForm({ child, children, onSave }: { child: Child; children: ReactNode; onSave: (name: string) => void }) {
  const [name, setName] = useState(child.name);
  return (
    <form
      id="child-rename"
      className="grid gap-5"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(name);
      }}
    >
      {children}
      <Input label="Name" value={name} onChange={(e) => { setName(e.target.value); }} maxLength={40} required />
    </form>
  );
}
