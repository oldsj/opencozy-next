// @dsCard group="People"
import { pointsLabel, Button, List, ListRow, ManageNav, ManageShell, PageHeader, PointsBadge, ProfileAvatar, Section, Tag } from '../../src';
import type { TagTone } from '../../src';

import { history, historyChild, type LedgerEntry, type LedgerKind } from './fixtures-points';
import { manageGroups } from './fixtures-manage';

export function Values() {
  return (
    <div className="flex items-center gap-3">
      <PointsBadge points={1} />
      <PointsBadge points={4} />
      <PointsBadge points={24} />
      <PointsBadge points={150} />
    </div>
  );
}

export function InContext() {
  return (
    <div className="flex items-center gap-2">
      <strong className="font-heading">Ada</strong>
      <PointsBadge points={24} />
    </div>
  );
}

const kindTags: Record<LedgerKind, { label: string; tone: TagTone }> = {
  earned: { label: 'Earned', tone: 'positive' },
  reversed: { label: 'Reversed', tone: 'attention' },
  redeemed: { label: 'Spent', tone: 'info' },
  refunded: { label: 'Refunded', tone: 'neutral' },
};

function signed(points: number): string {
  return points > 0 ? `+${String(points)}` : `−${String(Math.abs(points))}`;
}

function LedgerRow({ entry }: { entry: LedgerEntry }) {
  const undone = entry.reversedBy !== undefined;
  const tag = kindTags[entry.kind];
  const meta = [pointsLabel(entry.points, signed(entry.points)), entry.at, entry.by === undefined ? null : `by ${entry.by}`]
    .filter(Boolean)
    .join(' · ');
  return (
    <ListRow
      stackOnNarrow
      muted={undone}
      title={entry.title}
      meta={undone ? `${meta} · reversed` : meta}
      trailing={
        <>
          <Tag tone={tag.tone}>{tag.label}</Tag>
          {entry.kind === 'earned' && !undone ? (
            <Button variant="quiet" aria-label={`Reverse: ${entry.title}, ${entry.at}`}>
              Reverse…
            </Button>
          ) : null}
        </>
      }
    />
  );
}

/**
 * One child's points history for adults, built only from `history` (fixtures-points.ts). Earned
 * points can be reversed; the reversal is its own entry and the original stays, marked reversed.
 */
export function PointsHistory() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/children')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            <ProfileAvatar name={historyChild.name} color={historyChild.color} size="small" aria-hidden="true" />
            {historyChild.name}’s points
          </span>
        }
        description="Every change, newest first. A reversal never deletes the original."
        actions={<PointsBadge points={history.balance} />}
      />
      <Section title="History">
        <List>
          {history.entries.map((entry) => (
            <LedgerRow key={entry.id} entry={entry} />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}
