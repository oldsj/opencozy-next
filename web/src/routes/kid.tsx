import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';

import { AppFrame, useSurface } from '@app/design';

import { client } from '../api/client';
import { TodayBoard } from './today';

const sessionKey = ['kid', 'session'] as const;

// /kid: the kid view in the iPad app's web view, signed in as the device (its __Host-device
// cookie, from /device/session). Only the device's own child: their day and their rewards.
//
// When the session has ended (any 401: expired, unpaired, reassigned), the page goes to
// /device/expired. The app watches for that navigation and opens a fresh link; if the device
// itself is no longer paired, the app shows its own "ask an adult" screen.
export function KidPage() {
  useSurface('kiosk');
  const navigate = useNavigate();
  const session = useQuery({
    queryKey: sessionKey,
    queryFn: async () => {
      const { data, response } = await client.GET('/device/session');
      if (response.status === 401) return null;
      if (!data) throw new Error('The session could not be loaded.');
      return data;
    },
    retry: 2,
    refetchInterval: 5 * 60_000,
  });
  const ended = () => { void navigate({ to: '/device/expired', replace: true }); };
  const gone = session.data === null;
  useEffect(() => {
    if (gone) void navigate({ to: '/device/expired', replace: true });
  }, [gone, navigate]);

  if (session.isPending || gone) {
    return (
      <AppFrame aria-busy="true" contentClassName="grid place-content-center">
        <p className="m-0 text-muted-foreground">Getting your day…</p>
      </AppFrame>
    );
  }
  if (!session.data) {
    return (
      <AppFrame contentClassName="grid place-content-center">
        <p className="m-0 text-muted-foreground">Couldn’t load your day. It will try again.</p>
      </AppFrame>
    );
  }
  return <TodayBoard household={session.data.household} kid={session.data.child} onSignedOut={ended} />;
}

// /device/expired: the one-time link was used or too old. The app sees this page and makes a
// new link, so it stays quiet.
export function DeviceExpiredPage() {
  useSurface('kiosk');
  return (
    <AppFrame contentClassName="grid place-content-center">
      <p className="m-0 text-muted-foreground">One moment…</p>
    </AppFrame>
  );
}
