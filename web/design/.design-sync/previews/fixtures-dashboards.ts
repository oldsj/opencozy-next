/**
 * Slice 16 (dashboard views), shaped like the old app's typed widget registry (fictional data). A
 * view is a named grid of up to 24 widgets; each widget has a type, a schema version, its config
 * and x/y/w/h on a 6-column grid. A widget the server can't validate comes back valid: false and is
 * shown as an isolated broken card, never breaking the rest of the view.
 */
export type WidgetType = 'today' | 'calendar' | 'meals' | 'home' | 'tvs' | 'weather';

export interface WidgetSize {
  w: number;
  h: number;
}

export interface WidgetDescriptor {
  type: WidgetType;
  title: string;
  description: string;
  /** False: addable and saved, but its data isn't built yet (shows a coming-later tile). */
  available: boolean;
  defaultSize: WidgetSize;
  minSize: WidgetSize;
  maxSize: WidgetSize;
}

export const registry: WidgetDescriptor[] = [
  { type: 'today', title: 'Today', description: 'The children’s chores and routines.', available: true, defaultSize: { w: 4, h: 3 }, minSize: { w: 3, h: 2 }, maxSize: { w: 6, h: 6 } },
  { type: 'calendar', title: 'Calendar', description: 'Today’s events from the family calendars.', available: true, defaultSize: { w: 2, h: 2 }, minSize: { w: 2, h: 1 }, maxSize: { w: 6, h: 6 } },
  { type: 'meals', title: 'Meals', description: 'Today’s meal plan.', available: true, defaultSize: { w: 2, h: 1 }, minSize: { w: 2, h: 1 }, maxSize: { w: 6, h: 6 } },
  { type: 'home', title: 'Home', description: 'Readings you share from Home Assistant.', available: true, defaultSize: { w: 2, h: 1 }, minSize: { w: 1, h: 1 }, maxSize: { w: 6, h: 6 } },
  { type: 'tvs', title: 'TVs', description: 'Who’s watching, and the time left.', available: true, defaultSize: { w: 2, h: 2 }, minSize: { w: 2, h: 1 }, maxSize: { w: 6, h: 6 } },
  { type: 'weather', title: 'Weather', description: 'The day outside.', available: false, defaultSize: { w: 2, h: 1 }, minSize: { w: 1, h: 1 }, maxSize: { w: 6, h: 3 } },
];

export interface ViewWidget {
  id: string;
  type: string;
  schemaVersion: number;
  config: { title?: string | undefined };
  x: number;
  y: number;
  w: number;
  h: number;
  valid: boolean;
  /** Why, when valid is false. */
  reason?: string | undefined;
}

export interface DashboardView {
  id: string;
  name: string;
  version: number;
  archived: boolean;
  widgets: ViewWidget[];
  /** Displays showing this view. */
  displays: string[];
}

export const kitchen: DashboardView = {
  id: 'v1',
  name: 'Kitchen',
  version: 7,
  archived: false,
  displays: ['Kitchen display'],
  widgets: [
    { id: 'w1', type: 'today', schemaVersion: 1, config: {}, x: 0, y: 0, w: 4, h: 3, valid: true },
    { id: 'w2', type: 'calendar', schemaVersion: 1, config: {}, x: 4, y: 0, w: 2, h: 2, valid: true },
    { id: 'w3', type: 'meals', schemaVersion: 1, config: {}, x: 4, y: 2, w: 2, h: 1, valid: true },
    { id: 'w4', type: 'home', schemaVersion: 1, config: {}, x: 0, y: 3, w: 3, h: 2, valid: true },
    { id: 'w5', type: 'tvs', schemaVersion: 1, config: {}, x: 3, y: 3, w: 3, h: 2, valid: true },
  ],
};

export const hallway: DashboardView = {
  id: 'v2',
  name: 'Hallway',
  version: 2,
  archived: false,
  displays: [],
  widgets: [
    { id: 'w6', type: 'calendar', schemaVersion: 1, config: {}, x: 0, y: 0, w: 3, h: 2, valid: true },
    { id: 'w7', type: 'home', schemaVersion: 1, config: { title: 'Doors' }, x: 3, y: 0, w: 3, h: 1, valid: true },
    { id: 'w8', type: 'clock', schemaVersion: 1, config: {}, x: 3, y: 1, w: 3, h: 1, valid: false, reason: 'Unknown widget type.' },
  ],
};

export const views: DashboardView[] = [kitchen, hallway, { id: 'v3', name: 'Summer', version: 4, archived: true, displays: [], widgets: [] }];

export const MAX_WIDGETS = 24;

export const dashboardErrorText = {
  version_conflict: 'Someone else changed this view while you were editing. It now shows their version; your changes weren’t saved.',
  too_many: `A view holds up to ${String(MAX_WIDGETS)} widgets.`,
  broken: 'This widget can’t be shown here. Remove it, or add it again.',
} as const;

export function descriptorFor(type: string): WidgetDescriptor | undefined {
  return registry.find((d) => d.type === type);
}
