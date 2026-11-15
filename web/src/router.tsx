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
import { NotesPage } from './routes/notes';

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
const notesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.query({ ...meQuery, staleTime: 'static' });
    // eslint-disable-next-line @typescript-eslint/only-throw-error -- TanStack Router's redirect is thrown by design
    if (!me) throw redirect({ to: '/login' });
    return { me };
  },
  component: NotesPage,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
});

const routeTree = rootRoute.addChildren([notesRoute, loginRoute]);

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient }, defaultPreload: 'intent' });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
