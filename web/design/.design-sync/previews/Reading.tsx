// @dsCard group="Dashboard"
import { DoorOpen, Lock, Thermometer, WashingMachine } from 'lucide-react';

import { Reading } from '../../src';

export function Default() {
  return <Reading label="Kitchen" value="71" unit="°F" icon={Thermometer} />;
}

/** How current: fresh, the last good value with its age, can't read, not read yet. */
export function States() {
  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
      <Reading label="Kitchen" value="71" unit="°F" icon={Thermometer} />
      <Reading label="Washing machine" value="Running" icon={WashingMachine} status="stale" note="As of 3:52 PM" />
      <Reading label="Garden rain" status="error" note="Home Assistant didn’t answer" />
      <Reading label="Solar power" status="pending" />
    </div>
  );
}

/** Worth a glance from across the room: a door left open, a lock left unlocked. */
export function Attention() {
  return (
    <div className="grid grid-cols-2 gap-4">
      <Reading label="Back door" value="Open" icon={DoorOpen} tone="attention" />
      <Reading label="Front door" value="Locked" icon={Lock} />
    </div>
  );
}
