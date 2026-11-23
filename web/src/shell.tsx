import { useQueryClient } from '@tanstack/react-query';
import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router';
import {
  CalendarDays,
  ClipboardCheck,
  Gift,
  Hourglass,
  House,
  Inbox,
  KeyRound,
  LogOut,
  MonitorSmartphone,
  Plug,
  Settings,
  Smartphone,
  Sparkles,
  Tv,
  UserRound,
  Users,
  Utensils,
  BookOpen,
} from 'lucide-react';

import { Button, ManageNav, ManageShell, Notice, type ManageNavGroup, type ManageNavItem } from '@app/design';

import { $api, errorMessage, meQuery, type Me } from './api/client';
import { TV_REFRESH_MS } from './routes/tv';

type Item = Omit<ManageNavItem, 'current'> & {
  /** Other pages that belong to this section (a child's points under Children). */
  also?: readonly RegExp[];
};

// Grouped by how often a parent reaches for them: the children's day, then the household's
// information, then setup done once.
const groups: readonly { label: string; items: readonly Item[] }[] = [
  {
    label: 'Family',
    items: [
      { href: '/', label: 'Overview', icon: House },
      { href: '/inbox', label: 'Inbox', icon: Inbox },
      { href: '/children', label: 'Children', icon: Users, also: [/^\/children\//] },
      { href: '/chores', label: 'Chores', icon: ClipboardCheck },
      { href: '/rewards', label: 'Rewards', icon: Gift, also: [/^\/redemptions$/] },
      { href: '/screen-time', label: 'Screen time', icon: Hourglass },
      { href: '/learning', label: 'Learning', icon: Sparkles },
    ],
  },
  {
    label: 'Home',
    items: [
      { href: '/calendar', label: 'Calendar', icon: CalendarDays, also: [/^\/calendar\//, /^\/calendars$/] },
      { href: '/meals', label: 'Meals', icon: Utensils },
      { href: '/recipes', label: 'Recipes', icon: BookOpen, also: [/^\/recipes\//] },
      { href: '/tvs', label: 'TVs', icon: Tv },
    ],
  },
  {
    label: 'Setup',
    items: [
      { href: '/displays', label: 'Displays', icon: MonitorSmartphone },
      { href: '/devices', label: 'Devices', icon: Smartphone },
      { href: '/connections', label: 'Connections', icon: Plug },
      { href: '/household', label: 'Household', icon: Settings },
    ],
  },
];

/** How many things wait for an adult: the inbox's items and the TVs asking who's watching. */
export function useWaiting(enabled = true) {
  const inbox = $api.useQuery('get', '/inbox', {}, { enabled, refetchInterval: TV_REFRESH_MS });
  if (!inbox.data) return undefined;
  return inbox.data.items.length + inbox.data.tvs.tvs.filter((t) => t.prompt).length;
}

// The signed-in adult's frame: the grouped navigation (a rail on wide screens, a bar with Menu on
// a phone), their account and Sign out, and the page.
export function AdultShell({ me }: { me: Me }) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const waiting = useWaiting();
  // Account has only single sign-on so far: link it when that is set up.
  const sso = $api.useQuery('get', '/auth/methods').data?.oidc;
  const navGroups: ManageNavGroup[] = groups.map((g) => {
    const items = g.label === 'Setup' && sso ? [...g.items, { href: '/account', label: 'Account', icon: KeyRound }] : g.items;
    return {
      label: g.label,
      items: items.map(({ also, ...item }: Item) => ({
        ...item,
        current: item.href === path || (also?.some((re) => re.test(path)) ?? false),
        ...(item.href === '/inbox' && waiting ? { badge: waiting } : {}),
      })),
    };
  });
  return (
    <ManageShell
      nav={
        <ManageNav
          groups={navGroups}
          shortcut="/inbox"
          renderLink={({ href, target, ...props }) => <Link to={href} {...props} {...(target ? { target } : {})} />}
          footer={<AccountFooter me={me} />}
        />
      }
    >
      <Outlet />
    </ManageShell>
  );
}

function AccountFooter({ me }: { me: Me }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const logout = $api.useMutation('post', '/auth/logout', {
    onSuccess: () => {
      // Nothing of the household stays in memory once signed out.
      queryClient.clear();
      queryClient.setQueryData(meQuery.queryKey, null);
      void navigate({ to: '/login' });
    },
  });
  return (
    <div className="grid gap-2">
      <p className="m-0 flex items-center gap-2 px-3 text-meta text-muted-foreground">
        <UserRound aria-hidden="true" size={18} />
        <span className="min-w-0 truncate">{me.adult.name}</span>
      </p>
      {logout.error && <Notice tone="error">{errorMessage(logout.error)}</Notice>}
      <Button variant="quiet" disabled={logout.isPending} onClick={() => { logout.mutate({}); }}>
        <LogOut aria-hidden="true" size={18} />
        {logout.isPending ? 'Signing out…' : 'Sign out'}
      </Button>
    </div>
  );
}
