// @dsCard group="Layout"
import { CalendarDays, Gift, House, Settings } from 'lucide-react';

import { AppFrame, Button, StatusPill, TabBar, TopBar } from '../../src';

export function Default() {
  return (
    <AppFrame
      topBar={
        <TopBar
          time="10:53"
          date="Sunday, September 13"
          householdName="Maple House"
          status={<StatusPill status="reconnecting" />}
          actions={
            <Button aria-label="Open family settings">
              <Settings aria-hidden="true" size={22} />
            </Button>
          }
        />
      }
      tabBar={
        <TabBar
          items={[
            { href: '#today', label: 'Today', icon: House, current: true },
            { href: '#rewards', label: 'Rewards', icon: Gift },
            { href: '#calendar', label: 'Calendar', icon: CalendarDays },
          ]}
        />
      }
    >
      <p>Family route content fills this working area.</p>
    </AppFrame>
  );
}
