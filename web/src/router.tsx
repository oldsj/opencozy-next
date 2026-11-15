import { useQuery, type QueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import {
  Link,
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
  useMatches,
} from '@tanstack/react-router';

import { meQuery, onSignedOut } from './api/client';
import { LoginPage } from './routes/login';
import { HomePage } from './routes/home';
import { DisplayPage } from './routes/display';
import { ChoresPage } from './routes/chores';
import { InboxPage } from './routes/inbox';
import { PointsPage } from './routes/points';
import { RewardsPage } from './routes/rewards';
import { DevicesPage } from './routes/devices';
import { DisplaysPage } from './routes/displays';
import { DeviceExpiredPage, KidPage } from './routes/kid';
import { MealsPage } from './routes/meals';
import { RecipePage, RecipesPage } from './routes/recipes';

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

const nav = [
  { to: '/', label: 'Home' },
  { to: '/inbox', label: 'Inbox' },
  { to: '/chores', label: 'Chores' },
  { to: '/rewards', label: 'Rewards' },
  { to: '/meals', label: 'Meals' },
  { to: '/recipes', label: 'Recipes' },
  { to: '/displays', label: 'Displays' },
  { to: '/devices', label: 'Devices' },
] as const;

// The manage pages' column, with the main navigation once signed in.
function PageLayout() {
  const me = useQuery(meQuery).data;
  return (
    <>
      {me && (
        <header className="mx-auto max-w-2xl px-4 pt-6">
          <nav aria-label="Main" className="flex flex-wrap gap-x-5 gap-y-2">
            {nav.map((n) => (
              <Link key={n.to} to={n.to} activeOptions={{ exact: n.to === '/' }} activeProps={{ className: 'font-semibold' }}>
                {n.label}
              </Link>
            ))}
          </nav>
        </header>
      )}
      <main className="mx-auto max-w-2xl px-4 py-10">
        <Outlet />
      </main>
    </>
  );
}

// Every page except the wall display sits in one reading column.
const pageRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'page',
  component: PageLayout,
});

// Signed-in pages: redirect to /login when there is no session. The server enforces this
// independently; this only spares the user a page of errors.
const homeRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/',
  staticData: { title: 'Home' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
    return { me };
  },
  component: function Home() {
    return <HomePage me={homeRoute.useRouteContext().me} />;
  },
});

const loginRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/login',
  staticData: { title: 'Sign in' },
  // ?ended=true: sent here because the session ended.
  validateSearch: (search: Record<string, unknown>): { ended?: boolean } => (search.ended === true ? { ended: true } : {}),
  component: function Login() {
    return <LoginPage ended={loginRoute.useSearch().ended === true} />;
  },
});

// Manage: the household's wall displays.
const displaysRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/displays',
  staticData: { title: 'Displays' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: DisplaysPage,
});

// Manage: children and their chores.
const choresRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/chores',
  staticData: { title: 'Chores' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: ChoresPage,
});

// Manage: approve what the children finished.
const inboxRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/inbox',
  staticData: { title: 'Inbox' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: InboxPage,
});

// Manage: one child's points history.
const pointsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/children/$childId/points',
  staticData: { title: 'Points' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: function Points() {
    return <PointsPage childId={pointsRoute.useParams().childId} />;
  },
});

// Manage: the week's meals, and the recipes they use.
const mealsRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/meals',
  staticData: { title: 'Meals' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: MealsPage,
});

const recipesRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/recipes',
  staticData: { title: 'Recipes' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: RecipesPage,
});

const recipeRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/recipes/$recipeId',
  staticData: { title: 'Recipe' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
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
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: RewardsPage,
});

// Manage: the children's iPads and phones.
const devicesRoute = createRoute({
  getParentRoute: () => pageRoute,
  path: '/devices',
  staticData: { title: 'Devices' },
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
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
  pageRoute.addChildren([homeRoute, loginRoute, displaysRoute, choresRoute, inboxRoute, pointsRoute, mealsRoute, recipesRoute, recipeRoute, rewardsRoute, devicesRoute]),
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
