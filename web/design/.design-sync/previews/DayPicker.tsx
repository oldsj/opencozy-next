// @dsCard group="Forms"
import { useState } from 'react';

import { DayPicker } from '../../src';

export function Default() {
  const [days, setDays] = useState<number[]>([1, 3, 5]);
  return <DayPicker label="Days for Tidy the hallway" value={days} onChange={setDays} />;
}

/** The API stores every day as an empty list; every day shows pressed. */
export function EveryDay() {
  const [days, setDays] = useState<number[]>([]);
  return <DayPicker label="Days for Feed the cat" value={days} onChange={setDays} />;
}

/** One day left: it can't be turned off. */
export function OneDay() {
  const [days, setDays] = useState<number[]>([3]);
  return <DayPicker label="Days for Bins out" value={days} onChange={setDays} />;
}
