// @dsCard group="Rewards"
import { AppFrame, Button, Notice, PageHeader, PointsBadge, RewardTile, Sheet, TopBar, profileClass } from '../../src';

import { rewards, type RedemptionResult } from './fixtures-points';

const noop = () => undefined;
const balance = 24;

export function Tiles() {
  return (
    <div className={`${profileClass('sky')} grid grid-cols-3 gap-4`}>
      <RewardTile name="Movie night" cost={50} balance={12} />
      <RewardTile name="Choose a game" cost={4} balance={5} />
      <RewardTile name="Late bedtime" cost={30} balance={2} disabled />
    </div>
  );
}

function KioskRewardsPage({ children }: { children?: React.ReactNode }) {
  return (
    <AppFrame person="sky" topBar={<TopBar time="4:05" date="Monday, September 14" householdName="Maple House" />}>
      <PageHeader title="Abby’s rewards" description="Choose one, then an adult says yes." actions={<PointsBadge points={balance} />} />
      {children}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {rewards.rewards.map((reward) => (
          <RewardTile key={reward.id} name={reward.title} cost={reward.cost} balance={balance} disabled={!reward.available} onSelect={noop} />
        ))}
      </div>
    </AppFrame>
  );
}

/** The child's rewards on the kiosk (view with ?surface=kiosk), from `rewards` (fixtures-points.ts). */
export function KioskRewards() {
  return <KioskRewardsPage />;
}

/** Choosing a reward: one confirmation. The points are spent now and refunded if an adult says no. */
export function KioskRedeem() {
  return (
    <>
      <KioskRewardsPage />
      <Sheet
        open
        onClose={noop}
        title="Choose 20 minutes of iPad?"
        description="This uses 10 of Abby’s 24 points. If an adult says no, the points come back."
        footer={
          <>
            <Button>Not now</Button>
            <Button variant="primary">Ask for it</Button>
          </>
        }
      />
    </>
  );
}

const sent: RedemptionResult = { id: 'd1', status: 'pending' };

/** After asking: the request waits in the adult inbox. */
export function KioskRedeemSent() {
  return (
    <KioskRewardsPage>
      {sent.status === 'pending' ? (
        <Notice tone="info" title="Asked for 20 minutes of iPad">
          An adult will see it in the inbox. If they say no, you get 10 points back.
        </Notice>
      ) : null}
    </KioskRewardsPage>
  );
}
