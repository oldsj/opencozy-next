import type { QueryClient } from '@tanstack/react-query';
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router';

import { meQuery } from './api/client';
import { LoginPage } from './routes/login';
import { HomePage } from './routes/home';
import { DisplayPage } from './routes/display';
import { ChoresPage } from './routes/chores';
import { InboxPage } from './routes/inbox';
import { PointsPage } from './routes/points';
import { RewardsPage } from './routes/rewards';
import { DisplaysPage } from './routes/displays';

interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <main className="mx-auto max-w-2xl px-4 py-10">
      <Outlet />
    </main>
  ),
});

// Signed-in pages: redirect to /login when there is no session. The server enforces this
// independently; this only spares the user a page of errors.
const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
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
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
});

// Manage: the household's wall displays.
const displaysRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/displays',
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: DisplaysPage,
});

// Manage: children and their chores.
const choresRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/chores',
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: ChoresPage,
});

// Manage: approve what the children finished.
const inboxRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/inbox',
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: InboxPage,
});

// Manage: one child's points history.
const pointsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/children/$childId/points',
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: function Points() {
    return <PointsPage childId={pointsRoute.useParams().childId} />;
  },
});

// Manage: rewards.
const rewardsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/rewards',
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
  },
  component: RewardsPage,
});

// The wall display itself: pairs, then shows the household's day. No adult sign-in.
const displayRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/display',
  component: DisplayPage,
});

const routeTree = rootRoute.addChildren([homeRoute, loginRoute, displaysRoute, choresRoute, inboxRoute, pointsRoute, rewardsRoute, displayRoute]);

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient }, defaultPreload: 'intent' });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
