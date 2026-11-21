import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { Button, CheckCircle, ConfirmDialog, EmptyState, Input, List, ListRow, Notice, PageHeader, ProfileAvatar, Section, SegmentedControl, Sheet } from '@app/design';

import { $api, errorMessage, idempotencyKey, isAnswer, useAttempts, type Child } from '../api/client';
import type { components, paths } from '../api/schema.gen';
import { useReauth } from './reauth';
import { ruleMeta } from './learning';

type Credential = components['schemas']['Credential'];
type Kind = Credential['kind'];
type NewCredential = paths['/credentials']['post']['requestBody']['content']['application/json'];

const LEARNING = 'learning:award';

// What each scope lets a connection do, in words for adults.
const scopeLabels: Record<string, string> = {
  'household:read': 'See the household’s name and settings',
  'children:read': 'See the children, their points and their day',
  [LEARNING]: 'Give points for learning, by your award rules',
  'tv:gate': 'Let gated TVs online (your router)',
  'tv:report': 'Report TVs turning on and off (Home Assistant)',
  'screentime:read': 'See the children’s iPad time',
  'games:gate': 'Open game websites only while a child’s iPad time runs (a router)',
  'today:read': 'See today’s chores and routines',
  'today:complete': 'Tick off chores and routine steps as you (points land at once)',
  'inbox:read': 'See what waits for your decision',
  'inbox:decide': 'Approve, send back and answer requests for you',
  'points:read': 'See each child’s points history',
  'rewards:read': 'See rewards and what the children redeemed',
  'calendar:read': 'See the family calendar',
  'meals:read': 'See the meal plan',
  'screentime:grant': 'Give and end iPad time as you',
  'tv:read': 'See the TVs, who is watching and TV history',
  'tv:manage': 'Add, edit and remove TVs',
  'tv:control': 'Let TVs online, turn them off, open and close them, as you',
};

// Which scopes each kind is offered. An assistant acts as its adult, for every child.
const kindScopes: Record<Kind, string[]> = {
  service: ['household:read', 'children:read', 'screentime:read', LEARNING, 'games:gate', 'tv:gate', 'tv:report'],
  assistant: [
    'household:read', 'children:read', 'today:read', 'today:complete', 'inbox:read', 'inbox:decide',
    'points:read', 'rewards:read', 'calendar:read', 'meals:read', 'screentime:read', 'screentime:grant', 'tv:read', 'tv:manage', 'tv:control',
  ],
};

const kindLabels: Record<Kind, { label: string; detail: string }> = {
  service: { label: 'Service', detail: 'A home server or automation, such as a router or a smart-home hub.' },
  assistant: { label: 'Assistant', detail: 'An AI assistant (over MCP) that helps with the family’s day, as you.' },
};

const dateLabel = (iso: string) => new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

function toggle(list: string[], id: string): string[] {
  return list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
}

// Manage: service and assistant credentials. Each sees only what it was given; revoking stops it at once.
export function ConnectionsPage() {
  const queryClient = useQueryClient();
  const credentials = $api.useQuery('get', '/credentials');
  const children = $api.useQuery('get', '/children');
  const rules = $api.useQuery('get', '/learning/rules', {});
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/credentials'] });
  // gcTime 0: the response holds the key, so it doesn't stay in the mutation cache.
  const create = $api.useMutation('post', '/credentials', { onSuccess: refresh, gcTime: 0 });
  const revoke = $api.useMutation('delete', '/credentials/{credentialId}', { onSuccess: refresh });
  const attempts = useAttempts();
  const reauth = useReauth();

  const [adding, setAdding] = useState(false);
  const [kind, setKind] = useState<Kind>('service');
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<string[]>([]);
  // Which children a key may act for is always picked on purpose: none until chosen.
  const [all, setAll] = useState(false);
  const [childIds, setChildIds] = useState<string[]>([]);
  const [ruleIds, setRuleIds] = useState<string[]>([]);
  const [secret, setSecret] = useState<{ name: string; secret: string } | null>(null);
  const [copied, setCopied] = useState(false);
  const [revoking, setRevoking] = useState<Credential | null>(null);

  const kids = children.data?.items ?? [];
  const live: Child[] = kids.filter((c) => !c.archived);
  const liveRules = rules.data?.rules ?? [];
  const learning = scopes.includes(LEARNING);
  const childName = (id: string) => kids.find((k) => k.id === id)?.name ?? 'A child';
  const ruleKey = (id: string) => liveRules.find((r) => r.id === id)?.activityKey ?? 'an archived rule';

  const close = () => {
    setAdding(false);
    setName('');
    setScopes([]);
    setAll(false);
    setChildIds([]);
    setRuleIds([]);
    create.reset();
  };

  const issue = (body: NewCredential) => {
    const attempt = JSON.stringify(body);
    create.mutate(
      { params: attempts.params(attempt), body },
      {
        onSettled: attempts.settleIfAnswered(attempt),
        onSuccess: (r) => {
          close();
          setCopied(false);
          setSecret({ name: r.credential.name, secret: r.secret });
        },
        onError: (error) => {
          // No answer: it may have been created; show the list as it is now.
          if (!isAnswer(error)) void refresh();
          reauth.ask(error, () => { issue(body); });
        },
      },
    );
  };

  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    const usable = scopes.filter((s) => kindScopes[kind].includes(s));
    const everyone = all || kind === 'assistant';
    issue({
      kind,
      name: name.trim(),
      scopes: usable,
      allChildren: everyone,
      childIds: everyone ? [] : childIds,
      ...(usable.includes(LEARNING) ? { awardRuleIds: ruleIds } : {}),
    });
  };

  const list = credentials.data?.items ?? [];
  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title="Connections"
        description="Home servers, assistants and learning services. Each has its own key and sees only what you allow."
        actions={<Button variant="primary" onClick={() => { setAdding(true); }}>Add a service</Button>}
      />
      {list.length ? (
        <Section title="Connected" count={list.length}>
          <List aria-label="Connections">
            {list.map((c) => (
              <ListRow
                key={c.id}
                stackOnNarrow
                title={c.name}
                meta={[
                  kindLabels[c.kind].label,
                  c.scopes.map((s) => scopeLabels[s] ?? s).join('; '),
                  c.allChildren ? 'All children' : c.childIds.map(childName).join(', '),
                  c.awardRuleIds.length > 0 ? `awards ${c.awardRuleIds.map(ruleKey).join(', ')}` : null,
                  `added ${dateLabel(c.createdAt)}`,
                  c.expiresAt === undefined ? null : `expires ${dateLabel(c.expiresAt)}`,
                ]
                  .filter(Boolean)
                  .join(' · ')}
                trailing={
                  <Button variant="danger" aria-label={`Revoke ${c.name}`} onClick={() => { setRevoking(c); }}>
                    Revoke…
                  </Button>
                }
              />
            ))}
          </List>
        </Section>
      ) : (
        credentials.isSuccess && <EmptyState title="Nothing connected yet" description="Add a home server, an assistant or a learning service." variant="surface" />
      )}

      <Sheet
        open={adding}
        onClose={close}
        title="Add a connection"
        description="You’ll see its key once, on the next screen."
        footer={
          <>
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" type="submit" form="add-connection" disabled={create.isPending}>
              Create key
            </Button>
          </>
        }
      >
        <form id="add-connection" onSubmit={submit} className="grid gap-5" aria-label="Add a connection">
          <SegmentedControl
            label="Kind"
            value={kind}
            onChange={(k) => { setKind(k); setScopes((s) => s.filter((x) => kindScopes[k].includes(x))); }}
            options={[
              { value: 'service', label: kindLabels.service.label },
              { value: 'assistant', label: kindLabels.assistant.label },
            ]}
          />
          <p className="m-0 text-muted-foreground">{kindLabels[kind].detail}</p>
          <Input label="Name" hint="So you recognise it later, such as Router." maxLength={80} value={name} onChange={(e) => { setName(e.target.value); }} required />
          <Section title="It can">
            <List>
              {kindScopes[kind].map((scope) => (
                <ListRow
                  key={scope}
                  title={scopeLabels[scope] ?? scope}
                  meta={scope}
                  trailing={<CheckCircle checked={scopes.includes(scope)} label={scopeLabels[scope] ?? scope} onClick={() => { setScopes((s) => toggle(s, scope)); }} />}
                />
              ))}
            </List>
          </Section>
          {learning && (
            <Section title="Award rules it can use" description="It can give points only by these rules, up to each rule’s daily limit.">
              {liveRules.length ? (
                <List>
                  {liveRules.map((r) => (
                    <ListRow
                      key={r.id}
                      title={r.activityKey}
                      meta={ruleMeta(r)}
                      trailing={<CheckCircle checked={ruleIds.includes(r.id)} label={`Use ${r.activityKey}`} onClick={() => { setRuleIds((ids) => toggle(ids, r.id)); }} />}
                    />
                  ))}
                </List>
              ) : (
                <Notice tone="attention">Add an award rule on the Learning page first.</Notice>
              )}
            </Section>
          )}
          {kind === 'assistant' ? (
            <p className="m-0 text-muted-foreground">It acts as you, for every child. What it does shows as yours, through this assistant.</p>
          ) : (
          <SegmentedControl
            label="For"
            value={all ? 'all' : 'some'}
            onChange={(v) => { setAll(v === 'all'); }}
            options={[
              { value: 'all', label: 'All children' },
              { value: 'some', label: 'Choose children' },
            ]}
          />
          )}
          {!all && kind !== 'assistant' && (
            <List aria-label="Children">
              {live.map((c) => (
                <ListRow
                  key={c.id}
                  leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
                  title={c.name}
                  trailing={<CheckCircle checked={childIds.includes(c.id)} label={c.name} onClick={() => { setChildIds((ids) => toggle(ids, c.id)); }} />}
                />
              ))}
            </List>
          )}
          {create.error && !reauth.needsPassword(create.error) && <Notice tone="error">{errorMessage(create.error)}</Notice>}
        </form>
      </Sheet>

      <Sheet
        open={secret !== null}
        onClose={() => { setSecret(null); }}
        title={secret ? `Key for ${secret.name}` : ''}
        description="Copy it into the service now. OpenCozy won’t show it again; if you lose it, revoke this one and add another."
        footer={<Button variant="primary" onClick={() => { setSecret(null); }}>Done</Button>}
      >
        <div className="grid gap-3">
          <Input label="Key" readOnly value={secret?.secret ?? ''} className="font-mono" onFocus={(e) => { e.currentTarget.select(); }} />
          <div>
            <Button
              onClick={() => {
                if (secret) void navigator.clipboard.writeText(secret.secret).then(() => { setCopied(true); });
              }}
            >
              {copied ? 'Copied' : 'Copy key'}
            </Button>
          </div>
        </div>
      </Sheet>

      <ConfirmDialog
        open={revoking !== null}
        title={revoking ? `Revoke ${revoking.name}?` : ''}
        description="It stops working at once. To connect it again, add it as a new service."
        confirmLabel="Revoke"
        tone="danger"
        pending={revoke.isPending}
        error={revoke.error ? errorMessage(revoke.error) : undefined}
        onCancel={() => { setRevoking(null); revoke.reset(); }}
        onConfirm={() => {
          if (!revoking) return;
          revoke.mutate({ params: { ...idempotencyKey(), path: { credentialId: revoking.id } } }, { onSuccess: () => { setRevoking(null); } });
        }}
      />
      {reauth.dialog}
    </section>
  );
}
