// @dsCard group="Feedback"
import { Tag } from '../../src';

export function Tones() {
  return (
    <div className="flex flex-wrap gap-2">
      <Tag>Canceled · 25 back</Tag>
      <Tag tone="attention">Needs attention</Tag>
      <Tag tone="positive">Connected</Tag>
      <Tag tone="info">Paused</Tag>
      <Tag tone="error">Revoked</Tag>
    </div>
  );
}
