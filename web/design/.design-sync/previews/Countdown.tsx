// @dsCard group="Screen time"
import { Countdown, Tag } from '../../src';

export function Inline() {
  return (
    <p>
      On · Ada · <Countdown seconds={1499} />
    </p>
  );
}

export function InATag() {
  return (
    <div className="flex flex-wrap gap-2">
      <Tag tone="info">
        <Countdown seconds={3723} />
      </Tag>
      <Tag tone="attention">
        <Countdown seconds={0} />
      </Tag>
    </div>
  );
}

export function Large() {
  return <Countdown size="large" seconds={1800} />;
}
