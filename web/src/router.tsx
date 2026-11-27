import { useQuery, type QueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
  useMatches,
} from '@tanstack/react-router';

import { meQuery, onSignedOut } from './api/client';
import { AdultShell } from './shell';
import { LoginPage, isSsoError, type SsoError } from './routes/login';
import { AccountPage } from './routes/account';
import { HomePage } from './routes/home';
import { DisplayPage } from './routes/display';
import { ChildrenPage } from './routes/children';
import { ChoresPage } from './routes/chores';
import { InboxPage } from './routes/inbox';
import { PointsPage } from './routes/points';
import { RewardsPage } from './routes/rewards';
import { ScreenTimePage } from './routes/screen-time';
import { RedemptionsPage } from './routes/redemptions';
import { HouseholdPage } from './routes/household';
import { DevicesPage } from './routes/devices';
import { DisplaysPage } from './routes/displays';
import { DeviceExpiredPage, KidPage } from './routes/kid';
import { MealsPage } from './routes/meals';
import { RecipePage, RecipesPage } from './routes/recipes';
import { CalendarEventPage, CalendarPage, CalendarsPage } from './routes/calendar';
import { googleOutcomes, type GoogleOutcome } from './routes/calendar-google';
import { ConnectionsPage } from './routes/connections';
import { LearningPage } from './routes/learning';
import { TvsPage } from './routes/tvs';

interface RouterContext {
  queryClient: QueryClient;
}

// Every page names itself in the tab: its route's staticData.title.
function Root() {
  const matches = useMatches();
  const title = matches.findLast((m) => m.staticData.title)?.staticData.title;
  useEffect(() => {
    document.title = title ? `${title} · OpenCozy` : 'OpenCozy';
  }, [title]);
  return <Outlet />;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Root,
});

// Signed-in pages: redirect to /login when there is no session. The server enforces this
// independently; this only spares the user a page of errors.
async function signedIn({ context }: { context: RouterContext }) {
  const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
  // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
  if (!me) throw redirect({ to: '/login' });
  return { me };
}

// Signed in: the adult shell, with its navigation. Signed out (the sign-in page): one quiet column.
function PageLayout() {
  const me = useQuery(meQuery).data;
  if (me) return <AdultShell me={me} />;
  return (
    <main className="mx-auto max-w-2xl px-4 py-10">
      <Outlet />
    </main>
  );
}

// Every page except the wall display sits in one reading column.
const pageRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'page',
  component: PageLayout,
});

// The adult's overview: what's waiting, each child's day, the screens.
const homeRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/',
  staticData: { title: 'Overview' },
  beforeLoad: signedIn,
  component: function Home() {
    return <HomePage me={homeRoute.useRouteContext().me} />;
  },
});

const loginRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/login',
  staticData: { title: 'Sign in' },
  // ?ended=true: sent here because the session ended. ?error=<code>: back from single sign-on.
  validateSearch: (search: Record<string, unknown>): { ended?: boolean; error?: SsoError } => ({
    ...(search.ended === true ? { ended: true } : {}),
    ...(isSsoError(search.error) ? { error: search.error } : {}),
  }),
  component: function Login() {
    const search = loginRoute.useSearch();
    return <LoginPage ended={search.ended === true} ssoError={search.error} />;
  },
});

// Manage: the household's wall displays.
const displaysRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/displays',
  staticData: { title: 'Displays' },
  beforeLoad: signedIn,
  component: DisplaysPage,
});

// Manage: children and their chores.
const choresRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/chores',
  staticData: { title: 'Chores' },
  beforeLoad: signedIn,
  component: ChoresPage,
});

// Manage: approve what the children finished.
const inboxRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/inbox',
  staticData: { title: 'Inbox' },
  beforeLoad: signedIn,
  component: InboxPage,
});

// Manage: the children, each with their points.
const childrenRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/children',
  staticData: { title: 'Children' },
  beforeLoad: signedIn,
  component: ChildrenPage,
});

// Manage: one child's points history.
const pointsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/children/$childId/points',
  staticData: { title: 'Points' },
  beforeLoad: signedIn,
  component: function Points() {
    return <PointsPage childId={pointsRoute.useParams().childId} />;
  },
});

// Manage: the week's meals, and the recipes they use.
const mealsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/meals',
  staticData: { title: 'Meals' },
  beforeLoad: signedIn,
  component: MealsPage,
});

const recipesRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/recipes',
  staticData: { title: 'Recipes' },
  beforeLoad: signedIn,
  component: RecipesPage,
});

const recipeRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/recipes/$recipeId',
  staticData: { title: 'Recipe' },
  beforeLoad: signedIn,
  component: function Recipe() {
    const { recipeId } = recipeRoute.useParams();
    return <RecipePage key={recipeId} recipeId={recipeId} />;
  },
});

// Manage: rewards.
const rewardsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/rewards',
  staticData: { title: 'Rewards' },
  beforeLoad: signedIn,
  component: RewardsPage,
});

// Manage: rewards chosen and approved, to give or cancel; and the household's settings.
const redemptionsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/redemptions',
  staticData: { title: 'Chosen rewards' },
  beforeLoad: signedIn,
  component: RedemptionsPage,
});
// Manage: the children's iPad time, allowed hours and blocked websites.
const screenTimeRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/screen-time',
  staticData: { title: 'Screen time' },
  beforeLoad: signedIn,
  component: ScreenTimePage,
});
// Account: how the signed-in adult signs in.
const accountRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/account',
  staticData: { title: 'Account' },
  beforeLoad: signedIn,
  component: AccountPage,
});
const householdRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/household',
  staticData: { title: 'Household' },
  beforeLoad: signedIn,
  component: HouseholdPage,
});

// Manage: the week's events, one event and who it's for, and the followed calendars.
const calendarRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/calendar',
  staticData: { title: 'Calendar' },
  beforeLoad: signedIn,
  component: CalendarPage,
});

const calendarEventRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/calendar/events/$eventId',
  staticData: { title: 'Event' },
  beforeLoad: signedIn,
  component: function CalendarEvent() {
    const { eventId } = calendarEventRoute.useParams();
    return <CalendarEventPage key={eventId} eventId={eventId} />;
  },
});

const calendarsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/calendars',
  staticData: { title: 'Calendars' },
  // ?google=<outcome>&connection=<id>: back from connecting Google.
  validateSearch: (search: Record<string, unknown>): { google?: GoogleOutcome; connection?: string } => {
    const google = googleOutcomes.find((o) => o === search.google);
    const connection = typeof search.connection === 'string' && /^[0-9a-f-]{36}$/.test(search.connection) ? search.connection : undefined;
    return google ? { google, ...(connection ? { connection } : {}) } : {};
  },
  beforeLoad: signedIn,
  component: function Calendars() {
    const { google, connection } = calendarsRoute.useSearch();
    return <CalendarsPage google={google} connection={connection} />;
  },
});

// Manage: services, assistants and learning apps, each with its own key.
const connectionsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/connections',
  staticData: { title: 'Connections' },
  beforeLoad: signedIn,
  component: ConnectionsPage,
});

// Manage: learning award rules and players.
const learningRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/learning',
  staticData: { title: 'Learning' },
  beforeLoad: signedIn,
  component: LearningPage,
});

// Manage: the TVs, who is watching, and opening them.
const tvsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/tvs',
  staticData: { title: 'TVs' },
  beforeLoad: signedIn,
  component: TvsPage,
});

// Manage: the children's iPads and phones.
const devicesRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/devices',
  staticData: { title: 'Devices' },
  beforeLoad: signedIn,
  component: DevicesPage,
});

// The kid view in a child's iPad app (signed in as the device), and where a used link lands.
const kidRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/kid',
  staticData: { title: 'Today' },
  component: KidPage,
});
const deviceExpiredRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/device/expired',
  staticData: { title: 'One moment' },
  component: DeviceExpiredPage,
});

// The wall display itself: pairs, then shows the household's day. No adult sign-in.
const displayRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/display',
  staticData: { title: 'Wall display' },
  component: DisplayPage,
});

const routeTree = rootRoute.addChildren([
  pageRoute.addChildren([homeRoute, loginRoute, displaysRoute, childrenRoute, choresRoute, inboxRoute, pointsRoute, mealsRoute, recipesRoute, recipeRoute, rewardsRoute, calendarRoute, calendarEventRoute, calendarsRoute, connectionsRoute, learningRoute, devicesRoute, redemptionsRoute, screenTimeRoute, householdRoute, tvsRoute, accountRoute]),
  displayRoute,
  kidRoute,
  deviceExpiredRoute,
]);

export function createAppRouter(queryClient: QueryClient) {
  const router = createRouter({ routeTree, context: { queryClient }, defaultPreload: 'intent' });
  // A 401 on a signed-in page: forget the session and sign in again.
  onSignedOut(() => {
    if (queryClient.getQueryData(meQuery.queryKey) === null) return;
    queryClient.setQueryData(meQuery.queryKey, null);
    void router.navigate({ to: '/login', search: { ended: true } });
  });
  return router;
}

declare module '@tanstack/react-router' {
  interface StaticDataRouteOption {
    title?: string;
  }
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
