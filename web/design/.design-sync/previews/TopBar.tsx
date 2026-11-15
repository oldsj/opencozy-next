// @dsCard group="Layout"
import { Settings } from 'lucide-react';

import { Button, StatusPill, TopBar } from '../../src';

export function Default() {
  return <TopBar time="7:41" period="AM" date="Tuesday, March 3" householdName="Maple House" />;
}

export function Offline() {
  return (
    <TopBar
      time="10:53"
      period="PM"
      date="Sunday, September 13"
      householdName="Maple House"
      status={<StatusPill status="offline" />}
      actions={
        <Button aria-label="Open family settings">
          <Settings aria-hidden="true" size={22} />
        </Button>
      }
    />
  );
}
