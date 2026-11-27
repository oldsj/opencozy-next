import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ConfirmDialog, List, ListRow, Notice, PageHeader, PointsBadge, pointsLabel, ProfileAvatar, Section, Tag } from '@app/design';
import type { TagTone } from '@app/design';

import { $api, errorMessage, idempotencyKey, type HistoryEntry } from '../api/client';

const kindTags: Record<HistoryEntry['kind'], { label: string; tone: TagTone }> = {
  earned: { label: 'Earned', tone: 'positive' },
  reversed: { label: 'Reversed', tone: 'attention' },
  redeemed: { label: 'Spent', tone: 'info' },
  refunded: { label: 'Refunded', tone: 'neutral' },
  one_more_minute: { label: 'iPad', tone: 'neutral' },
};

const when = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });

function signed(points: number): string {
  return points > 0 ? `+${String(points)}` : `−${String(Math.abs(points))}`;
}

// Manage: one child's points, newest first. Earned points can be taken back; the reversal is
// its own entry and the original stays, marked reversed.
export function PointsPage({ childId }: { childId: string }) {
  const queryClient = useQueryClient();
  const path = { params: { path: { childId } } };
  const child = $api.useQuery('get', '/children/{childId}', path);
  const history = $api.useQuery('get', '/children/{childId}/history', path);
  const reverse = $api.useMutation('post', '/points/{entryId}/reverse', {
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['get', '/children/{childId}/history', path] }),
  });
  const [reversing, setReversing] = useState<HistoryEntry | null>(null);

  const name = child.data?.name ?? '';
  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            {child.data && <ProfileAvatar name={name} color={child.data.color} size="small" aria-hidden="true" />}
            {name}’s points
          </span>
        }
        description="Every change, and each ask for one more minute on the iPad, newest first. A reversal never deletes the original."
        actions={history.data && <PointsBadge points={history.data.balance} />}
      />
      {history.isError && <Notice tone="error">{errorMessage(history.error)}</Notice>}
      {history.data && (
        <Section title="History">
          <List aria-label="Points history">
            {history.data.entries.map((entry) => {
              const undone = entry.reversedBy !== undefined;
              const tag = kindTags[entry.kind];
              const at = when.format(new Date(entry.at));
              // A shield event moves no points; it says where it happened instead.
              const points = entry.kind === 'one_more_minute' ? null : pointsLabel(entry.points, signed(entry.points));
              const meta = [points, at, entry.by === undefined ? null : `by ${entry.by}`, entry.device === undefined ? null : `on ${entry.device}`, entry.reason ?? null]
                .filter(Boolean)
                .join(' · ');
              return (
                <ListRow
                  key={entry.id}
                  stackOnNarrow
                  muted={undone}
                  title={entry.title}
                  meta={undone ? `${meta} · reversed` : meta}
                  trailing={
                    <>
                      <Tag tone={tag.tone}>{tag.label}</Tag>
                      {entry.kind === 'earned' && !undone ? (
                        <Button variant="quiet" aria-label={`Reverse: ${entry.title}, ${at}`} onClick={() => { reverse.reset(); setReversing(entry); }}>
                          Reverse…
                        </Button>
                      ) : null}
                    </>
                  }
                />
              );
            })}
          </List>
        </Section>
      )}
      <ConfirmDialog
        open={reversing !== null}
        title={reversing ? `Take back ${pointsLabel(reversing.points)} for ${reversing.title}?` : ''}
        description={`${name} loses the points, and the chore is open again today. The original stays in the history.`}
        confirmLabel="Take back"
        tone="danger"
        pending={reverse.isPending}
        error={reverse.error ? errorMessage(reverse.error) : undefined}
        onCancel={() => { setReversing(null); }}
        onConfirm={() => {
          if (!reversing) return;
          reverse.mutate(
            { params: { ...idempotencyKey(), path: { entryId: reversing.id } }, body: {} },
            { onSuccess: () => { setReversing(null); } },
          );
        }}
      />
    </>
  );
}
