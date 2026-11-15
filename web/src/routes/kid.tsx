import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Lock } from 'lucide-react';

import { AppFrame, EmptyState, useSurface } from '@app/design';

import { client } from '../api/client';
import { TodayBoard } from './today';

const sessionKey = ['kid', 'session'] as const;

// /kid: the kid view in the iPad app's web view, signed in as the device (its __Host-device
// cookie, from /device/session). Only the device's own child: their day and their rewards.
export function KidPage() {
  useSurface('kiosk');
  const queryClient = useQueryClient();
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
  const ended = () => { queryClient.setQueryData(sessionKey, null); };

  if (session.isPending) {
    return (
      <AppFrame aria-busy="true" contentClassName="grid place-content-center">
        <p className="m-0 text-muted-foreground">Getting your day…</p>
      </AppFrame>
    );
  }
  if (!session.data) {
    // Ended, unpaired, or given to another child: nothing to tap, a calm instruction.
    return (
      <AppFrame contentClassName="grid place-content-center">
        <EmptyState icon={Lock} title="Ask an adult to check this iPad" description="It needs to be connected to OpenCozy again." />
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
