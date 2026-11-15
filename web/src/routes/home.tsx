import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';

import { Button, EmptyState, List, ListRow, PageHeader } from '@app/design';

import { $api, meQuery, type Me } from '../api/client';

// A placeholder home until the family screens arrive: who is signed in, and the children.
export function HomePage({ me }: { me: Me }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const children = $api.useQuery('get', '/children');
  const logout = $api.useMutation('post', '/auth/logout', {
    onSuccess: () => {
      queryClient.setQueryData(meQuery.queryKey, null);
      void navigate({ to: '/login' });
    },
  });

  return (
    <section>
      <PageHeader
        title={me.household.name}
        description={`Signed in as ${me.adult.name}.`}
        actions={
          <>
          <Link to="/displays">Displays</Link>
          <Link to="/inbox">Inbox</Link>
          <Link to="/chores">Chores</Link>
          <Button onClick={() => { logout.mutate({}); }} disabled={logout.isPending}>
            Sign out
          </Button>
          </>
        }
      />
      {children.data?.items.length ? (
        <List aria-label="Children">
          {children.data.items.map((c) => (
            <ListRow key={c.id} title={c.name} />
          ))}
        </List>
      ) : (
        children.isSuccess && <EmptyState title="No children yet" />
      )}
    </section>
  );
}
