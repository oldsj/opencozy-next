import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, EmptyState, List, ListRow, Notice, PageHeader, pointsLabel, ProfileAvatar, Section, Sheet, Textarea } from '@app/design';

import { $api, errorMessage, idempotencyKey, type InboxItem } from '../api/client';
import { TV_REFRESH_MS } from './tv';
import { TvsAsking } from './tvs';

export const REJECT_REASON_MAX = 200;

const kindLabels: Record<InboxItem['kind'], string> = {
  completion: 'Chore',
  routine: 'Routine',
  redemption: 'Reward',
  time_request: 'Time request',
};

const when = new Intl.DateTimeFormat(undefined, { weekday: 'short', hour: 'numeric', minute: '2-digit' });

// Manage: what the children finished, oldest first. Approving pays the points; sending back
// leaves the chore on the child's list with the reason beside it.
export function InboxPage() {
  const queryClient = useQueryClient();
  // As often as the TVs page: a TV asking "Who's watching?" is waiting to go online.
  const inbox = $api.useQuery('get', '/inbox', {}, { refetchInterval: TV_REFRESH_MS });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/inbox'] });
  const approve = $api.useMutation('post', '/inbox/{itemId}/approve', { onSettled: refresh });
  const reject = $api.useMutation('post', '/inbox/{itemId}/reject', { onSettled: refresh });
  const [sendingBack, setSendingBack] = useState<InboxItem | null>(null);
  const [reason, setReason] = useState('');

  const busy = approve.isPending || reject.isPending;
  const error = approve.error ?? reject.error;
  const items = inbox.data?.items ?? [];

  const sendBack = () => {
    if (!sendingBack) return;
    const trimmed = reason.trim();
    reject.mutate(
      { params: { ...idempotencyKey(), path: { itemId: sendingBack.id } }, body: trimmed ? { reason: trimmed } : {} },
      { onSuccess: () => { setSendingBack(null); setReason(''); } },
    );
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Inbox" description="Approve what the children finished. Oldest first." />
      {error && <Notice tone="error">{errorMessage(error)}</Notice>}
      <TvsAsking list={inbox.data?.tvs} />
      {items.length > 0 ? (
        <Section title="Waiting for you" count={items.length}>
          <List aria-label="Waiting for you">
            {items.map((item) => {
              const meta = [item.child.name, item.points === undefined ? null : item.kind === 'redemption' ? `costs ${pointsLabel(item.points)}` : pointsLabel(item.points), kindLabels[item.kind], when.format(new Date(item.submittedAt))]
                .filter(Boolean)
                .join(' · ');
              return (
                <ListRow
                  key={item.id}
                  stackOnNarrow
                  leading={<ProfileAvatar name={item.child.name} color={item.child.color} size="small" aria-hidden="true" />}
                  title={item.title}
                  meta={meta}
                  trailing={
                    <>
                      <Button variant="danger" disabled={busy} aria-label={`Send back: ${item.title} (${item.child.name})`} onClick={() => { setSendingBack(item); }}>
                        Send back…
                      </Button>
                      <Button
                        variant="primary"
                        disabled={busy}
                        aria-label={`Approve: ${item.title} (${item.child.name})`}
                        onClick={() => { approve.mutate({ params: { ...idempotencyKey(), path: { itemId: item.id } }, body: {} }); }}
                      >
                        Approve
                      </Button>
                    </>
                  }
                />
              );
            })}
          </List>
        </Section>
      ) : (
        inbox.isSuccess && !inbox.data.tvs.tvs.length && <EmptyState title="Nothing waiting" description="When a child finishes a chore or routine, it shows here." variant="surface" />
      )}
      <Sheet
        open={sendingBack !== null}
        onClose={() => { setSendingBack(null); }}
        title={sendingBack ? `Send back ${sendingBack.title}?` : ''}
        description={
          sendingBack &&
          (sendingBack.kind === 'redemption'
            ? `${sendingBack.child.name} doesn’t get it, and the points come back.`
            : `${sendingBack.child.name} can tick it again. No points are given.`)
        }
        footer={
          <>
            <Button onClick={() => { setSendingBack(null); }}>Cancel</Button>
            <Button variant="danger" disabled={reject.isPending} onClick={sendBack}>Send back</Button>
          </>
        }
      >
        <Textarea
          label={sendingBack ? `Reason for ${sendingBack.child.name} (optional)` : 'Reason (optional)'}
          hint={`Shown on their list. Up to ${String(REJECT_REASON_MAX)} characters.`}
          maxLength={REJECT_REASON_MAX}
          value={reason}
          onChange={(e) => { setReason(e.target.value); }}
        />
      </Sheet>
    </section>
  );
}
