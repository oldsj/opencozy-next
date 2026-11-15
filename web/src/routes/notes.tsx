import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState, type SubmitEvent } from 'react';

import { Button, Input, Notice, Panel, Textarea } from '@app/design';

import { $api, errorMessage, meQuery, type Note } from '../api/client';
import { field } from '../api/form';

const listKey = ['get', '/notes'] as const;

export function NotesPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const notes = $api.useQuery('get', '/notes');
  const invalidate = () => queryClient.invalidateQueries({ queryKey: listKey });
  const create = $api.useMutation('post', '/notes', { onSuccess: invalidate });
  const logout = $api.useMutation('post', '/auth/logout', {
    onSuccess: () => {
      queryClient.setQueryData(meQuery.queryKey, null);
      queryClient.removeQueries({ queryKey: listKey });
      void navigate({ to: '/login' });
    },
  });

  function onCreate(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    create.mutate(
      { body: { title: field(data, 'title'), body: field(data, 'body') } },
      { onSuccess: () => { form.reset(); } },
    );
  }

  return (
    <section>
      <header className="mb-8 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Notes</h1>
        <Button onClick={() => { logout.mutate({}); }}>Sign out</Button>
      </header>

      <form onSubmit={onCreate} className="mb-8 space-y-3" aria-label="New note">
        <Input label="Title" name="title" required maxLength={200} />
        <Textarea label="Body" name="body" maxLength={20000} placeholder="Write something" />
        {create.isError && <Notice tone="error">{errorMessage(create.error)}</Notice>}
        <Button type="submit" variant="primary" disabled={create.isPending}>Add note</Button>
      </form>

      {notes.isPending && <Notice>Loading…</Notice>}
      {notes.isError && <Notice tone="error">{errorMessage(notes.error)}</Notice>}
      {notes.data?.items.length === 0 && <Notice>No notes yet.</Notice>}
      <ul className="space-y-3">
        {notes.data?.items.map((n) => <NoteItem key={n.id} note={n} onChanged={invalidate} />)}
      </ul>
    </section>
  );
}

function NoteItem({ note, onChanged }: { note: Note; onChanged: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const update = $api.useMutation('put', '/notes/{noteId}', {
    onSuccess: async () => { await onChanged(); setEditing(false); },
  });
  const remove = $api.useMutation('delete', '/notes/{noteId}', { onSuccess: onChanged });
  const params = { path: { noteId: note.id } };

  if (editing) {
    return (
      <Panel as="li">
        <form
          aria-label={`Edit ${note.title}`}
          className="space-y-2"
          onSubmit={(e) => {
            e.preventDefault();
            const d = new FormData(e.currentTarget);
            update.mutate({ params, body: { title: field(d, 'title'), body: field(d, 'body') } });
          }}
        >
          <Input label="Title" name="title" required maxLength={200} defaultValue={note.title} />
          <Textarea label="Body" name="body" maxLength={20000} defaultValue={note.body} />
          {update.isError && <Notice tone="error">{errorMessage(update.error)}</Notice>}
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={update.isPending}>Save</Button>
            <Button onClick={() => { setEditing(false); }}>Cancel</Button>
          </div>
        </form>
      </Panel>
    );
  }
  return (
    <Panel as="li">
      <h2 className="font-medium">{note.title}</h2>
      {note.body && <p className="text-muted-foreground mt-1 whitespace-pre-wrap">{note.body}</p>}
      {remove.isError && <Notice tone="error" className="mt-2">{errorMessage(remove.error)}</Notice>}
      <div className="mt-3 flex gap-2">
        <Button onClick={() => { setEditing(true); }}>Edit</Button>
        <Button variant="danger" disabled={remove.isPending} onClick={() => { remove.mutate({ params }); }}>
          Delete
        </Button>
      </div>
    </Panel>
  );
}
