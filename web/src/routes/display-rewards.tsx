import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ChoiceGroup, Notice, PageHeader, PointsBadge, pointsLabel, RewardTile, Sheet } from '@app/design';

import { client, errorMessage, useAttempts, type Today } from '../api/client';
import { screenTimeChanged } from '../api/native';

type Reward = { id: string; title: string; cost: number; available: boolean };

const rewardsKey = ['display', 'rewards'] as const;

// The wall display's Rewards: a child picks who they are, then a reward. The points are spent
// at once and come back if an adult says no. On a child's own device (`childId`), there is no
// choosing who.
export function DisplayRewards({ today, todayKey, childId: own }: { today: Today | undefined; todayKey: readonly unknown[]; childId?: string | undefined }) {
  const queryClient = useQueryClient();
  const [chosen, setChildId] = useState<string | null>(null);
  const childId = own ?? chosen;
  const [choosing, setChoosing] = useState<Reward | null>(null);
  const [sent, setSent] = useState<Reward | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [retry, setRetry] = useState<string | null>(null); // the sheet's own problem: try again
  const attempts = useAttempts('redeem');
  const rewards = useQuery({
    queryKey: rewardsKey,
    queryFn: async () => {
      const { data } = await client.GET('/rewards');
      if (!data) throw new Error('Rewards could not be loaded.');
      return data.rewards;
    },
    refetchInterval: 60_000,
  });

  const children = today?.children.map((c) => c.child) ?? [];
  const child = children.find((c) => c.id === childId);
  if (!child && own) return null; // the day hasn't loaded yet
  if (!child) {
    return (
      <section className="flex flex-col gap-6">
        <PageHeader title="Rewards" description="Who’s choosing?" />
        <ChoiceGroup
          label="Who’s choosing?"
          choices={children.map((c) => ({ value: c.id, label: c.name, detail: pointsLabel(c.points) }))}
          onChoose={(id) => { setChildId(id); setSent(null); setProblem(null); }}
        />
      </section>
    );
  }

  // One request per choice: a retry after a lost answer reuses its key, so the points are spent once.
  const attemptFor = (reward: Reward) => `${child.id}:${reward.id}`;
  const close = () => {
    setChoosing(null);
    setRetry(null);
  };
  // Cancel: a choice that was never sent, or was answered, is dropped. One whose answer was
  // lost keeps its key (in sessionStorage too), so choosing it again can't spend twice.
  const cancel = () => {
    if (choosing && !retry) attempts.settle(attemptFor(choosing));
    close();
  };
  const ask = async () => {
    if (!choosing) return;
    setBusy(true);
    setRetry(null);
    try {
      const { data, error, response } = await client.POST('/children/{childId}/redemptions', {
        params: { path: { childId: child.id }, ...attempts.params(attemptFor(choosing)) },
        body: { rewardId: choosing.id },
      });
      if (response.status >= 500) {
        // Maybe done, maybe not (a proxy's 502/504): keep the key and let them try again.
        setRetry('Couldn’t send that. Try again.');
        return;
      }
      attempts.settle(attemptFor(choosing));
      if (response.status === 201 && data) {
        // On the child's iPad, iPad time bought here lifts the shields now.
        if (own) screenTimeChanged();
        setSent(choosing);
        setProblem(null);
      } else {
        setSent(null);
        setProblem(errorMessage(error));
      }
      close();
      await queryClient.invalidateQueries({ queryKey: todayKey });
    } catch {
      setRetry('Couldn’t reach the server. Check the connection and try again.');
    } finally {
      setBusy(false);
    }
  };
  const short = choosing ? choosing.cost - child.points : 0;

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title={own ? 'Rewards' : `${child.name}’s rewards`}
        description="Choose one, then an adult says yes."
        actions={
          <>
            <PointsBadge points={child.points} />
            {!own && <Button onClick={() => { setChildId(null); }}>Someone else</Button>}
          </>
        }
      />
      {sent && (
        <Notice tone="info" title={`Asked for ${sent.title}`}>
          An adult will see it in the inbox. If they say no, you get {pointsLabel(sent.cost)} back.
        </Notice>
      )}
      {problem && <Notice tone="error">{problem}</Notice>}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {(rewards.data ?? []).map((reward) => (
          <RewardTile
            key={reward.id}
            name={reward.title}
            cost={reward.cost}
            balance={child.points}
            disabled={!reward.available || reward.cost > child.points}
            onSelect={() => { setChoosing(reward); }}
          />
        ))}
      </div>
      <Sheet
        open={choosing !== null}
        onClose={cancel}
        title={choosing ? `Choose ${choosing.title}?` : ''}
        description={
          choosing &&
          (short > 0
            ? `${child.name} needs ${pointsLabel(short)} more for this.`
            : `This uses ${String(choosing.cost)} of ${child.name}’s ${pointsLabel(child.points)}. If an adult says no, the points come back.`)
        }
        footer={
          <>
            <Button onClick={cancel}>Not now</Button>
            {short <= 0 && (
              <Button variant="primary" disabled={busy} onClick={() => void ask()}>Ask for it</Button>
            )}
          </>
        }
      >
        {retry && <Notice tone="error">{retry}</Notice>}
      </Sheet>
    </section>
  );
}
