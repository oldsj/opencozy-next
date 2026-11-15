import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ButtonPrimary, ConfirmDialog, EmptyState, List, ListRow, PageHeader, pointsLabel, ProfileAvatar, Section } from '@app/design';

import { $api, errorMessage, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';
import { EditError } from './edit-error';

type RedemptionRow = components['schemas']['RedemptionRow'];

const when = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });

/** What cancelling gives back, as the server works it out: all of it, or screen time's unused share rounded down. */
export function refundFor(r: Pick<RedemptionRow, 'status' | 'cost' | 'minutes' | 'usedMinutes'>): number {
  if (r.status === 'approved') return r.cost;
  if (r.status !== 'fulfilled' || r.minutes === undefined || r.minutes === 0) return 0;
  const left = r.minutes - Math.min(r.usedMinutes ?? 0, r.minutes);
  return Math.floor((r.cost * left) / r.minutes);
}

function meta(r: RedemptionRow): string {
  const at = r.cancelledAt ?? r.fulfilledAt ?? r.requestedAt;
  const used = r.minutes === undefined ? null : `${String(r.usedMinutes ?? 0)} of ${String(r.minutes)} min used`;
  const back = r.refundedPoints > 0 ? `${pointsLabel(r.refundedPoints)} back` : null;
  return [r.child.name, pointsLabel(r.cost), when.format(new Date(at)), used, back].filter(Boolean).join(' · ');
}

// Manage: rewards an adult said yes to, to give; what was given; what was cancelled. Cancelling
// gives the points back (screen time: the share not used yet).
export function RedemptionsPage() {
  const queryClient = useQueryClient();
  const list = $api.useQuery('get', '/redemptions');
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['get', '/redemptions'] });
    await queryClient.invalidateQueries({ queryKey: ['get', '/today'] });
  };
  const fulfil = $api.useMutation('post', '/redemptions/{redemptionId}/fulfil', { onSettled: refresh });
  const cancel = $api.useMutation('post', '/redemptions/{redemptionId}/cancel', { onSettled: refresh });
  const [cancelling, setCancelling] = useState<RedemptionRow | null>(null);
  const attempts = useAttempts();

  const rows = list.data?.redemptions ?? [];
  const toGive = rows.filter((r) => r.status === 'approved');
  const given = rows.filter((r) => r.status === 'fulfilled');
  const cancelled = rows.filter((r) => r.status === 'cancelled');
  const avatar = (r: RedemptionRow) => <ProfileAvatar name={r.child.name} color={r.child.color} size="small" aria-hidden="true" />;

  const give = (r: RedemptionRow) => {
    const attempt = `fulfil:${r.id}:${String(r.version)}`;
    fulfil.mutate(
      { params: { ...attempts.params(attempt), path: { redemptionId: r.id } }, body: { expectedVersion: r.version } },
      { onSettled: attempts.settleIfAnswered(attempt) },
    );
  };
  const confirmCancel = () => {
    if (!cancelling) return;
    const attempt = `cancel:${cancelling.id}:${String(cancelling.version)}`;
    cancel.mutate(
      { params: { ...attempts.params(attempt), path: { redemptionId: cancelling.id } }, body: { expectedVersion: cancelling.version } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setCancelling(null); } },
    );
  };
  const back = cancelling ? refundFor(cancelling) : 0;
  const partly = cancelling?.status === 'fulfilled';

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title="Chosen rewards" description="Approved rewards to give, and what’s been given." />
      <EditError error={fulfil.error} />
      {list.isSuccess && rows.length === 0 && <EmptyState title="Nothing chosen yet" description="Rewards you approve in the inbox show here, to give." variant="surface" />}
      {toGive.length > 0 && (
        <Section title="To give" count={toGive.length}>
          <List aria-label="To give">
            {toGive.map((r) => (
              <ListRow
                key={r.id}
                stackOnNarrow
                leading={avatar(r)}
                title={r.title}
                meta={meta(r)}
                trailing={
                  <>
                    <Button variant="quiet" aria-label={`Cancel ${r.title} for ${r.child.name} and refund`} onClick={() => { cancel.reset(); setCancelling(r); }}>
                      Cancel and refund…
                    </Button>
                    <ButtonPrimary aria-label={`Mark ${r.title} as given to ${r.child.name}`} disabled={fulfil.isPending} onClick={() => { give(r); }}>
                      Given
                    </ButtonPrimary>
                  </>
                }
              />
            ))}
          </List>
        </Section>
      )}
      {given.length > 0 && (
        <Section title="Given" count={given.length}>
          <List aria-label="Given">
            {given.map((r) => (
              <ListRow
                key={r.id}
                stackOnNarrow
                leading={avatar(r)}
                title={r.title}
                meta={meta(r)}
                trailing={
                  refundFor(r) > 0 ? (
                    <Button variant="quiet" aria-label={`Cancel the rest of ${r.title} for ${r.child.name}, ${pointsLabel(refundFor(r))} back`} onClick={() => { cancel.reset(); setCancelling(r); }}>
                      Cancel the rest…
                    </Button>
                  ) : undefined
                }
              />
            ))}
          </List>
        </Section>
      )}
      {cancelled.length > 0 && (
        <Section title="Cancelled" count={cancelled.length}>
          <List aria-label="Cancelled">
            {cancelled.map((r) => (
              <ListRow key={r.id} muted leading={avatar(r)} title={r.title} meta={meta(r)} />
            ))}
          </List>
        </Section>
      )}
      <ConfirmDialog
        open={cancelling !== null}
        title={cancelling ? (partly ? `Cancel the rest of ${cancelling.child.name}’s ${cancelling.title}?` : `Cancel ${cancelling.title} for ${cancelling.child.name}?`) : ''}
        description={
          cancelling
            ? partly
              ? `${cancelling.child.name} has used ${String(cancelling.usedMinutes ?? 0)} of ${String(cancelling.minutes ?? 0)} minutes. The minutes left are removed and ${cancelling.child.name} gets ${pointsLabel(back)} back.`
              : `${cancelling.child.name} gets ${pointsLabel(back)} back.`
            : ''
        }
        confirmLabel={`Cancel and refund ${pointsLabel(back)}`}
        cancelLabel="Keep it"
        tone="danger"
        pending={cancel.isPending}
        error={cancel.error ? errorMessage(cancel.error) : undefined}
        onCancel={() => { setCancelling(null); cancel.reset(); }}
        onConfirm={confirmCancel}
      />
    </section>
  );
}
