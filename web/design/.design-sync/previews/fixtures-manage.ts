import {
  CalendarDays,
  ClipboardCheck,
  Gift,
  History,
  House,
  Inbox,
  LayoutDashboard,
  ListChecks,
  MonitorSmartphone,
  Plug,
  Smartphone,
  Users,
  Utensils,
} from 'lucide-react';

import type { ManageNavGroup } from '../../src';

/** The adult areas, as the app groups them. `current` marks the page shown. */
export function manageGroups(current: string, waiting = 3): ManageNavGroup[] {
  const item = (href: string, label: string, icon: ManageNavGroup['items'][number]['icon'], badge?: number) => ({
    href,
    label,
    icon,
    current: href === current,
    ...(badge === undefined ? {} : { badge }),
  });
  return [
    {
      label: 'Family',
      items: [
        item('/manage/inbox', 'Inbox', Inbox, waiting),
        item('/manage/children', 'Children', Users),
        item('/manage/chores', 'Chores', ClipboardCheck),
        item('/manage/routines', 'Routines', ListChecks),
        item('/manage/rewards', 'Rewards', Gift),
        item('/manage/history', 'History', History),
      ],
    },
    {
      label: 'Home',
      items: [
        item('/manage/calendar', 'Calendars', CalendarDays),
        item('/manage/meals', 'Meals', Utensils),
        item('/manage/dashboard', 'Dashboard', LayoutDashboard),
      ],
    },
    {
      label: 'Setup',
      items: [
        item('/manage/displays', 'Displays', MonitorSmartphone),
        item('/manage/devices', 'Devices', Smartphone),
        item('/manage/integrations', 'Integrations', Plug),
        item('/manage/home-assistant', 'Home Assistant', House),
      ],
    },
  ];
}
