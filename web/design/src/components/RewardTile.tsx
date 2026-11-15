import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { Check, Gift } from 'lucide-react';

import { cx } from '../cx';
import { pointsLabel, pointsWord } from '../points';
import { ProgressBar } from './ProgressBar';

export interface RewardTileProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onSelect'> {
  name: string;
  cost: number;
  /** The child's current balance: drives the progress and the Ready state. */
  balance: number;
  onSelect?: () => void;
  icon?: ReactNode;
}


/**
 * A reward the child can work toward. It takes the width of its grid cell. Its accessible name is
 * its visible text ("Movie night 50 points 38 more points"); the bar repeats that, so it is hidden.
 */
export function RewardTile({ balance, className, cost, icon, name, onSelect, type = 'button', ...props }: RewardTileProps) {
  const safeCost = Math.max(1, cost);
  const ready = balance >= safeCost;
  const remaining = Math.max(0, safeCost - balance);
  const progress = Math.min(Math.max(0, balance), safeCost);
  const stateLabel = ready ? 'Ready' : `${String(remaining)} more ${pointsWord(remaining)}`;

  return (
    <button
      {...props}
      type={type}
      className={cx('oc-reward-tile', ready && 'oc-reward-tile--ready', className)}
      onClick={onSelect}
    >
      <span className="oc-reward-tile__icon" aria-hidden="true">
        {icon ?? <Gift size={28} strokeWidth={1.8} />}
      </span>
      <span className="oc-reward-tile__name">{name}</span>{' '}
      <span className="oc-reward-tile__cost">
        {pointsLabel(cost)}
      </span>{' '}
      <ProgressBar aria-hidden="true" className="oc-reward-tile__progress" value={progress} max={safeCost} label={`${name} progress: ${String(progress)} of ${pointsLabel(safeCost)}`} />
      <span className="oc-reward-tile__state">
        {ready && <Check aria-hidden="true" size={16} strokeWidth={2.4} />}
        {stateLabel}
      </span>
    </button>
  );
}
