import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ConfirmDialog, List, ListRow, Notice, PageHeader, Section } from '@app/design';

import { $api, errorMessage, meQuery } from '../api/client';
import { useReauth } from './reauth';

const day = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' });

// Account: how you sign in. Slice 10b brings single sign-on; passkeys (10a) and phones (11) join
// this page in their own slices.
export function AccountPage() {
  const me = useQuery(meQuery).data;
  const methods = $api.useQuery('get', '/auth/methods');
  const provider = methods.data?.oidc?.name;
  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Account" description={me ? `Signed in as ${me.adult.email}.` : undefined} />
      {provider !== undefined && <SingleSignOn provider={provider} email={me?.adult.email} />}
    </section>
  );
}

function SingleSignOn({ provider, email }: { provider: string; email: string | undefined }) {
  const queryClient = useQueryClient();
  const link = $api.useQuery('get', '/auth/oidc/link');
  const unlink = $api.useMutation('delete', '/auth/oidc/link', {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['get', '/auth/oidc/link'] }),
  });
  const reauth = useReauth();
  const [confirming, setConfirming] = useState(false);

  const doUnlink = () => {
    unlink.mutate({}, {
      onSuccess: () => { setConfirming(false); },
      onError: (error) => {
        if (reauth.ask(error, doUnlink)) setConfirming(false);
      },
    });
  };

  const linked = link.data?.link;
  return (
    <Section title="Single sign-on" description="Your password and passkeys keep working either way.">
      {link.isError && <Notice tone="error">{errorMessage(link.error)}</Notice>}
      {linked ? (
        <List>
          <ListRow
            stackOnNarrow
            title={`Linked to ${provider}`}
            meta={`${linked.email} · since ${day.format(new Date(linked.linkedAt))}`}
            trailing={
              <Button variant="quiet" aria-label={`Unlink ${provider}`} onClick={() => { unlink.reset(); setConfirming(true); }}>
                Unlink…
              </Button>
            }
          />
        </List>
      ) : (
        link.data && (
          <p className="m-0 text-muted-foreground">
            Not linked. Choose “Sign in with {provider}” on the sign-in page once; it links when {provider} confirms {email ?? 'your email'}.
          </p>
        )
      )}
      <ConfirmDialog
        open={confirming}
        title={`Unlink ${provider}?`}
        description={`“Sign in with ${provider}” stops working for you until you use it again. Your password and passkeys keep working.`}
        confirmLabel={`Unlink ${provider}`}
        tone="danger"
        pending={unlink.isPending}
        error={unlink.error && !reauth.needsPassword(unlink.error) ? errorMessage(unlink.error) : undefined}
        onCancel={() => { setConfirming(false); }}
        onConfirm={doUnlink}
      />
      {reauth.dialog}
    </Section>
  );
}
