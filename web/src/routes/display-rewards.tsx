import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ChoiceGroup, Notice, PageHeader, PointsBadge, RewardTile, Sheet } from '@app/design';

import { client, errorMessage, idempotencyKey, type Today } from '../api/client';

type Reward = { id: string; title: string; cost: number; available: boolean };

const rewardsKey = ['display', 'rewards'] as const;

// The wall display's Rewards: a child picks who they are, then a reward. The points are spent
// at once and come back if an adult says no.
export function DisplayRewards({ today, todayKey }: { today: Today | undefined; todayKey: readonly unknown[] }) {
  const queryClient = useQueryClient();
  const [childId, setChildId] = useState<string | null>(null);
  const [choosing, setChoosing] = useState<Reward | null>(null);
  const [sent, setSent] = useState<Reward | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
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
  if (!child) {
    return (
      <section className="flex flex-col gap-6">
        <PageHeader title="Rewards" description="Who’s choosing?" />
        <ChoiceGroup
          label="Who’s choosing?"
          choices={children.map((c) => ({ value: c.id, label: c.name, detail: `${String(c.points)} points` }))}
          onChoose={(id) => { setChildId(id); setSent(null); setProblem(null); }}
        />
      </section>
    );
  }

  const ask = async () => {
    if (!choosing) return;
    setBusy(true);
    const { data, error, response } = await client.POST('/children/{childId}/redemptions', {
      params: { path: { childId: child.id }, ...idempotencyKey() },
      body: { rewardId: choosing.id },
    });
    setBusy(false);
    if (response.status === 201 && data) {
      setSent(choosing);
      setProblem(null);
    } else {
      setProblem(errorMessage(error));
    }
    setChoosing(null);
    await queryClient.invalidateQueries({ queryKey: todayKey });
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title={`${child.name}’s rewards`}
        description="Choose one, then an adult says yes."
        actions={
          <>
            <PointsBadge points={child.points} />
            <Button onClick={() => { setChildId(null); }}>Someone else</Button>
          </>
        }
      />
      {sent && (
        <Notice tone="info" title={`Asked for ${sent.title}`}>
          An adult will see it in the inbox. If they say no, your {sent.cost} points come back.
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
            disabled={!reward.available}
            onSelect={() => { setChoosing(reward); }}
          />
        ))}
      </div>
      <Sheet
        open={choosing !== null}
        onClose={() => { setChoosing(null); }}
        title={choosing ? `Choose ${choosing.title}?` : ''}
        description={choosing ? `This uses ${String(choosing.cost)} of ${child.name}’s ${String(child.points)} points. If an adult says no, they come back.` : undefined}
        footer={
          <>
            <Button onClick={() => { setChoosing(null); }}>Not now</Button>
            <Button variant="primary" disabled={busy} onClick={() => void ask()}>Ask for it</Button>
          </>
        }
      />
    </section>
  );
}
