// @dsCard group="Layout"
import { CalendarDays, Gift, House, Utensils } from 'lucide-react';

import { TabBar } from '../../src';

// The bar is fixed to the viewport bottom; the empty box only gives the preview some height.
export function Default() {
  return (
    <div className="h-36">
      <TabBar
        items={[
          { href: '#today', label: 'Today', icon: House, current: true },
          { href: '#rewards', label: 'Rewards', icon: Gift },
          { href: '#meals', label: 'Meals', icon: Utensils },
          { href: '#calendar', label: 'Calendar', icon: CalendarDays },
        ]}
      />
    </div>
  );
}
