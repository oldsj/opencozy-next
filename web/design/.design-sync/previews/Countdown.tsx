// @dsCard group="Screen time"
import { Countdown, ProfileAvatar, Tag, profileClass } from '../../src';

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

export function Low() {
  return (
    <div className="grid gap-4">
      <p>
        On · Ada · <Countdown seconds={240} />
      </p>
      <Countdown size="large" seconds={240} />
    </div>
  );
}

/**
 * A TV's own screen while a child watches (view with ?surface=tv): who, and the time left, readable
 * from the sofa. It stays out of the way until the last five minutes.
 */
export function TvTimeLeft({ seconds = 240 }: { seconds?: number }) {
  return (
    <div className={`${profileClass('sky')} grid min-h-[60vh] place-content-center justify-items-center gap-6 text-center`}>
      <ProfileAvatar name="Ada" color="sky" />
      <p className="m-0 text-section">Ada’s TV time</p>
      <Countdown size="hero" seconds={seconds} />
      <p className="m-0 text-muted-foreground">{seconds > 0 ? 'Ask an adult on the iPad for more.' : 'The TV turns off in a moment.'}</p>
    </div>
  );
}

export function TvTimesUp() {
  return <TvTimeLeft seconds={0} />;
}
