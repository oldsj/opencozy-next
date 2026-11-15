import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type SyntheticEvent } from 'react';

import {
  Button,
  ButtonPrimary,
  ChoiceGroup,
  ConfirmDialog,
  Countdown,
  EmptyState,
  Input,
  List,
  ListRow,
  Notice,
  PageHeader,
  ProfileAvatar,
  QrCode,
  Section,
  Sheet,
  Textarea,
} from '@app/design';

import { $api, errorMessage, useAttempts, type Child, type Device, type DevicePairingOffer } from '../api/client';
import { useReauth } from './reauth';

const platformLabels: Record<Device['platform'], string> = { ios: 'iOS', android: 'Android' };

const day = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' });
const when = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });

/** Whole seconds until deadline (a Date.now() value), ticking once a second. */
function useSecondsLeft(deadline: number | null) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (deadline === null) return undefined;
    const t = window.setInterval(() => { setNow(Date.now()); }, 1000);
    return () => { window.clearInterval(t); };
  }, [deadline]);
  return deadline === null ? 0 : Math.max(0, Math.ceil((deadline - now) / 1000));
}

// Manage → Devices: the children's iPads and phones. Pairing starts here, per child; the device
// scans the QR code or takes the typed code, and this page notices it arrive.
export function DevicesPage() {
  const queryClient = useQueryClient();
  const children = $api.useQuery('get', '/children');
  const live = children.data?.items.filter((c) => !c.archived) ?? [];
  const [pairing, setPairing] = useState<{ child: Child | null; offer: DevicePairingOffer | null; deadline: number | null; known: string[] } | null>(null);
  const devices = $api.useQuery('get', '/devices', {}, { refetchInterval: pairing?.offer ? 3000 : false });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/devices'] });
  const offer = $api.useMutation('post', '/devices/pairings');
  const update = $api.useMutation('patch', '/devices/{deviceId}', { onSuccess: refresh });
  const revoke = $api.useMutation('delete', '/devices/{deviceId}', { onSuccess: refresh });
  const attempts = useAttempts();
  const reauth = useReauth();
  const [paired, setPaired] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<Device | null>(null);
  const [newName, setNewName] = useState('');
  const [reassigning, setReassigning] = useState<Device | null>(null);
  const [unpairing, setUnpairing] = useState<Device | null>(null);
  const secondsLeft = useSecondsLeft(pairing?.deadline ?? null);
  const list = devices.data?.devices ?? [];

  const startPairing = (child: Child) => {
    const known = list.map((d) => d.id);
    const attempt = `pair:${child.id}`;
    const ask = () => {
      offer.mutate(
        { params: attempts.params(attempt), body: { childId: child.id } },
        {
          onSettled: attempts.settleIfAnswered(attempt),
          // The offer lasts 5 minutes; count them on this clock, which may differ from the server's.
          onSuccess: (o) => { setPairing({ child, offer: o, deadline: Date.now() + 5 * 60 * 1000, known }); },
          onError: (error) => { reauth.ask(error, ask); },
        },
      );
    };
    setPaired(null);
    setPairing({ child, offer: null, deadline: null, known });
    ask();
  };

  // A device that wasn't there when the offer was made has paired: close the sheet.
  const arrived = pairing?.offer ? list.find((d) => !pairing.known.includes(d.id)) : undefined;
  const arrivedName = useRef<string | null>(null);
  useEffect(() => {
    if (arrived && arrivedName.current !== arrived.id) {
      arrivedName.current = arrived.id;
      setPaired(arrived.name);
      setPairing(null);
    }
  }, [arrived]);

  const rename = (e: SyntheticEvent) => {
    e.preventDefault();
    if (!renaming) return;
    const body = { name: newName };
    const attempt = `rename:${renaming.id}:${newName}`;
    update.mutate(
      { params: { ...attempts.params(attempt), path: { deviceId: renaming.id } }, body },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setRenaming(null); } },
    );
  };
  const reassign = (device: Device, childId: string) => {
    const attempt = `child:${device.id}:${childId}`;
    update.mutate(
      { params: { ...attempts.params(attempt), path: { deviceId: device.id } }, body: { childId } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setReassigning(null); } },
    );
  };
  const unpair = (device: Device) => {
    const attempt = `unpair:${device.id}`;
    revoke.mutate(
      { params: { ...attempts.params(attempt), path: { deviceId: device.id } } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setUnpairing(null); } },
    );
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title="Devices"
        description="The children’s iPads and phones. Each shows only its own child’s day."
        actions={
          <ButtonPrimary onClick={() => { setPaired(null); setPairing({ child: null, offer: null, deadline: null, known: [] }); }} disabled={live.length === 0}>
            Pair a device
          </ButtonPrimary>
        }
      />
      {paired && <Notice tone="success">{paired} is paired.</Notice>}
      {offer.error && !reauth.needsPassword(offer.error) && !pairing && <Notice tone="error">{errorMessage(offer.error)}</Notice>}
      <Section title="Paired" count={list.length}>
        {list.length === 0 ? (
          <EmptyState title="No devices yet" description="Pair a child’s iPad or phone, and it shows their day and rewards." />
        ) : (
          <List aria-label="Paired devices">
            {list.map((d) => (
              <ListRow
                key={d.id}
                stackOnNarrow
                muted={d.child === null}
                leading={d.child ? <ProfileAvatar name={d.child.name} color={d.child.color} size="small" aria-hidden="true" /> : undefined}
                title={d.name}
                meta={[
                  platformLabels[d.platform],
                  d.child ? `${d.child.name}’s` : 'Not assigned to a child',
                  `paired ${day.format(new Date(d.pairedAt))}`,
                  d.lastSeenAt ? `seen ${when.format(new Date(d.lastSeenAt))}` : 'not seen yet',
                ].join(' · ')}
                trailing={
                  <>
                    <Button variant="quiet" aria-label={`Rename ${d.name}`} onClick={() => { setNewName(d.name); setRenaming(d); }}>
                      Rename
                    </Button>
                    <Button variant="quiet" aria-label={`Change whose ${d.name} is`} onClick={() => { setReassigning(d); }}>
                      Change child
                    </Button>
                    <Button variant="danger" aria-label={`Unpair ${d.name}`} onClick={() => { setUnpairing(d); }}>
                      Unpair…
                    </Button>
                  </>
                }
              />
            ))}
          </List>
        )}
      </Section>
      {devices.data && <LockMessage current={devices.data.lockMessage ?? null} fallback={devices.data.defaultLockMessage} />}

      <Sheet
        open={pairing !== null}
        onClose={() => { setPairing(null); }}
        title={pairing?.child ? `Pair a device for ${pairing.child.name}` : 'Pair a device'}
        description={pairing?.child ? `On ${pairing.child.name}’s iPad, open the OpenCozy app and scan this code, or type it in.` : 'Whose device is it?'}
        footer={<Button onClick={() => { setPairing(null); }}>Cancel</Button>}
      >
        {pairing && !pairing.child && (
          <ChoiceGroup
            label="Child for the new device"
            onChoose={(id) => { const c = live.find((x) => x.id === id); if (c) startPairing(c); }}
            choices={live.map((c) => ({ value: c.id, label: c.name }))}
          />
        )}
        {pairing?.child && !pairing.offer && (offer.isPending ? <p className="m-0 text-muted-foreground">Making a code…</p> : offer.error && !reauth.needsPassword(offer.error) && <Notice tone="error">{errorMessage(offer.error)}</Notice>)}
        {pairing?.child && pairing.offer && (
          <div className="grid justify-items-center gap-5 text-center">
            {secondsLeft > 0 ? (
              <>
                <div className="w-60 max-w-full">
                  <QrCode modules={pairing.offer.qrModules.map((row) => Array.from({ length: row.length }, (_, i) => row[i] === '1'))} label={`Pairing code for ${pairing.child.name}’s device`} />
                </div>
                <p className="m-0 text-page font-semibold tabular-nums tracking-widest" aria-label={`Code ${pairing.offer.code.split('').join(' ')}`}>
                  {pairing.offer.code}
                </p>
                <p className="m-0 text-muted-foreground">
                  Waiting for the device · <Countdown seconds={secondsLeft} lowAt={60} />
                </p>
              </>
            ) : (
              <>
                <p className="m-0">This code has expired.</p>
                <ButtonPrimary onClick={() => { if (pairing.child) startPairing(pairing.child); }}>New code</ButtonPrimary>
              </>
            )}
          </div>
        )}
      </Sheet>

      <Sheet
        open={renaming !== null}
        onClose={() => { setRenaming(null); }}
        title={renaming ? `Rename ${renaming.name}` : ''}
        footer={
          <>
            <Button onClick={() => { setRenaming(null); }}>Cancel</Button>
            <ButtonPrimary type="submit" form="rename-device" disabled={update.isPending}>Save</ButtonPrimary>
          </>
        }
      >
        <form id="rename-device" onSubmit={rename} className="flex flex-col gap-4">
          <Input label="Name" value={newName} onChange={(e) => { setNewName(e.target.value); }} maxLength={80} required />
          {update.error && <Notice tone="error">{errorMessage(update.error)}</Notice>}
        </form>
      </Sheet>

      <Sheet
        open={reassigning !== null}
        onClose={() => { setReassigning(null); }}
        title={reassigning ? `Whose is ${reassigning.name}?` : ''}
        description="It switches to that child’s day straight away; no need to pair again."
        footer={<Button onClick={() => { setReassigning(null); }}>Cancel</Button>}
      >
        {reassigning && (
          <ChoiceGroup
            label={`Child for ${reassigning.name}`}
            busy={update.isPending}
            onChoose={(id) => { reassign(reassigning, id); }}
            choices={live.map((c) => ({ value: c.id, label: c.name }))}
          />
        )}
        {update.error && <Notice tone="error">{errorMessage(update.error)}</Notice>}
      </Sheet>

      <ConfirmDialog
        open={unpairing !== null}
        onCancel={() => { setUnpairing(null); }}
        onConfirm={() => { if (unpairing) unpair(unpairing); }}
        title={unpairing ? `Unpair ${unpairing.name}?` : ''}
        description={unpairing?.child ? `It signs out at once and shows the pairing screen. ${unpairing.child.name}’s points and history stay.` : 'It signs out at once and shows the pairing screen.'}
        confirmLabel={unpairing ? `Unpair ${unpairing.name}` : 'Unpair'}
        tone="danger"
      />
      {reauth.dialog}
    </section>
  );
}

// The words on a locked device's shield: the household's own, or the default.
function LockMessage({ current, fallback }: { current: string | null; fallback: string }) {
  const queryClient = useQueryClient();
  const [text, setText] = useState(current ?? fallback);
  const [saved, setSaved] = useState(false);
  const attempts = useAttempts();
  const save = $api.useMutation('put', '/devices/lock-message', {
    onSuccess: () => { setSaved(true); void queryClient.invalidateQueries({ queryKey: ['get', '/devices'] }); },
  });
  const send = (lockMessage: string | null) => {
    setSaved(false);
    const attempt = `lock:${lockMessage ?? ''}`;
    save.mutate({ params: attempts.params(attempt), body: { lockMessage } }, { onSettled: attempts.settleIfAnswered(attempt) });
  };
  return (
    <Section title="On a locked device" description="What a child sees when their device is locked.">
      <form
        className="flex flex-col gap-4"
        aria-label="Lock message"
        onSubmit={(e) => { e.preventDefault(); send(text.trim() === fallback ? null : text); }}
      >
        <Textarea label="Message" value={text} onChange={(e) => { setText(e.target.value); setSaved(false); }} maxLength={200} rows={2} required />
        {save.error && <Notice tone="error">{errorMessage(save.error)}</Notice>}
        {saved && <Notice tone="success">Saved.</Notice>}
        <div className="flex gap-3">
          <ButtonPrimary type="submit" disabled={save.isPending}>Save</ButtonPrimary>
          {current !== null && (
            <Button onClick={() => { setText(fallback); send(null); }} disabled={save.isPending}>
              Use the default
            </Button>
          )}
        </div>
      </form>
    </Section>
  );
}
