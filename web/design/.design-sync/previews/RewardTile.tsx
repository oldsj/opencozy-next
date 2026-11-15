// @dsCard group="Rewards"
import { RewardTile, profileClass } from '../../src';

export function Tiles() {
  return (
    <div className={`${profileClass('sky')} grid grid-cols-3 gap-4`}>
      <RewardTile name="Movie night" cost={50} balance={12} />
      <RewardTile name="Choose a game" cost={4} balance={5} />
      <RewardTile name="Late bedtime" cost={30} balance={2} disabled />
    </div>
  );
}
