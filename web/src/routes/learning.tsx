import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';
import { Link2, Sparkles } from 'lucide-react';

import { Button, ChoiceGroup, ConfirmDialog, EmptyState, Input, List, ListRow, Notice, PageHeader, pointsLabel, ProfileAvatar, Section, Sheet } from '@app/design';

import { $api, errorCode, errorMessage, idempotencyKey, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';

type AwardRule = components['schemas']['AwardRule'];
type LearningLink = components['schemas']['LearningLink'];

const PROVIDER = 'minecraft';
const providerNames: Record<string, string> = { minecraft: 'Minecraft' };

// The API's answers an adult can get here, in words.
const errorText: Record<string, string> = {
  rule_exists: 'A rule for that activity already exists. Edit it instead.',
  version_conflict: 'Someone else changed this rule. It now shows their version; check it and save again.',
  rule_archived: 'This rule was archived, so it can’t be changed. Add a new rule instead.',
  link_exists: 'That player ID is already linked to a child.',
};

export function ruleMeta(r: Pick<AwardRule, 'points' | 'dailyLimit'>): string {
  return `${pointsLabel(r.points)} each · up to ${pointsLabel(r.dailyLimit)} a day`;
}

const dateLabel = (iso: string) => new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

interface RuleForm {
  id?: string;
  version?: number;
  activityKey: string;
  points: string;
  dailyLimit: string;
}

const emptyRule: RuleForm = { activityKey: '', points: '5', dailyLimit: '20' };
const formOf = (r: AwardRule): RuleForm => ({ id: r.id, version: r.version, activityKey: r.activityKey, points: String(r.points), dailyLimit: String(r.dailyLimit) });

// Manage: what each learning activity is worth, and which player is which child. A learning
// service gives points only for a linked child, by a live rule, under its daily limit.
export function LearningPage() {
  const queryClient = useQueryClient();
  const rules = $api.useQuery('get', '/learning/rules', { params: { query: { archived: true } } });
  const links = $api.useQuery('get', '/learning/links');
  const children = $api.useQuery('get', '/children');
  const refreshRules = () => queryClient.invalidateQueries({ queryKey: ['get', '/learning/rules'] });
  const refreshLinks = () => queryClient.invalidateQueries({ queryKey: ['get', '/learning/links'] });
  const createRule = $api.useMutation('post', '/learning/rules', { onSuccess: refreshRules });
  const updateRule = $api.useMutation('put', '/learning/rules/{ruleId}', { onSuccess: refreshRules });
  const link = $api.useMutation('post', '/learning/links', { onSuccess: refreshLinks });
  const unlink = $api.useMutation('delete', '/learning/links/{linkId}', { onSuccess: refreshLinks });
  const attempts = useAttempts();

  const [editing, setEditing] = useState<RuleForm | null>(null);
  const [ruleError, setRuleError] = useState<string | null>(null);
  const [archivedMeanwhile, setArchivedMeanwhile] = useState(false);
  const [archiving, setArchiving] = useState<AwardRule | null>(null);
  const [linking, setLinking] = useState(false);
  const [linkChild, setLinkChild] = useState<string | null>(null);
  const [player, setPlayer] = useState('');
  const [unlinking, setUnlinking] = useState<LearningLink | null>(null);

  const all = rules.data?.rules ?? [];
  const live = all.filter((r) => !r.archived);
  const archived = all.filter((r) => r.archived);
  const kids = children.data?.items ?? [];
  const liveKids = kids.filter((c) => !c.archived);
  const childOf = (id: string) => kids.find((c) => c.id === id);

  const openRule = (form: RuleForm) => {
    setEditing(form);
    setRuleError(null);
    setArchivedMeanwhile(false);
    createRule.reset();
    updateRule.reset();
  };

  // A 409 on a rule: show it on the sheet. A stale version reloads the rule into the form.
  const onRuleError = (error: unknown) => {
    const code = errorCode(error);
    setRuleError((code !== undefined ? errorText[code] : undefined) ?? errorMessage(error));
    if (code === 'rule_archived') setArchivedMeanwhile(true);
    if (code === 'version_conflict' && editing?.id) {
      const id = editing.id;
      void rules.refetch().then((r) => {
        const fresh = r.data?.rules.find((x) => x.id === id);
        if (fresh) setEditing(formOf(fresh));
      });
    }
  };

  const saveRule = (e: SyntheticEvent) => {
    e.preventDefault();
    if (!editing) return;
    const fields = { activityKey: editing.activityKey.trim(), points: Number(editing.points), dailyLimit: Number(editing.dailyLimit) };
    setRuleError(null);
    if (editing.id === undefined || editing.version === undefined) {
      const attempt = JSON.stringify(fields);
      createRule.mutate(
        { params: attempts.params(attempt), body: fields },
        { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setEditing(null); }, onError: onRuleError },
      );
      return;
    }
    const body = { ...fields, archived: false, expectedVersion: editing.version };
    const attempt = JSON.stringify([editing.id, body]);
    updateRule.mutate(
      { params: { ...attempts.params(attempt), path: { ruleId: editing.id } }, body },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setEditing(null); }, onError: onRuleError },
    );
  };

  const saveLink = (e: SyntheticEvent) => {
    e.preventDefault();
    if (!linkChild) return;
    const body = { childId: linkChild, provider: PROVIDER, externalId: player.trim() };
    const attempt = JSON.stringify(body);
    link.mutate(
      { params: attempts.params(attempt), body },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setLinking(false); setPlayer(''); setLinkChild(null); } },
    );
  };

  const pending = createRule.isPending || updateRule.isPending;
  const linkError = link.error ? (errorCode(link.error) === 'link_exists' ? errorText.link_exists : errorMessage(link.error)) : undefined;
  const ruleKeyError = ruleError === errorText.rule_exists ? ruleError : undefined;

  return (
    <>
      <PageHeader title="Learning" description="Points for learning: what each activity is worth, and which player is which child." />

      <Section title="Award rules" count={live.length} actions={<Button variant="primary" onClick={() => { openRule(emptyRule); }}>Add a rule</Button>}>
        {live.length ? (
          <List aria-label="Award rules">
            {live.map((r) => (
              <ListRow
                key={r.id}
                stackOnNarrow
                title={<span className="font-mono">{r.activityKey}</span>}
                meta={ruleMeta(r)}
                trailing={
                  <>
                    <Button variant="quiet" aria-label={`Edit ${r.activityKey}`} onClick={() => { openRule(formOf(r)); }}>
                      Edit
                    </Button>
                    <Button variant="quiet" aria-label={`Archive ${r.activityKey}`} onClick={() => { setArchiving(r); }}>
                      Archive…
                    </Button>
                  </>
                }
              />
            ))}
          </List>
        ) : (
          rules.isSuccess && <EmptyState variant="surface" icon={Sparkles} title="No award rules yet" description="Add what an activity is worth before connecting a learning service." />
        )}
      </Section>

      {archived.length > 0 && (
        <Section title="Archived rules" count={archived.length} description="No longer awarded. Points already given stay.">
          <List aria-label="Archived rules">
            {archived.map((r) => (
              <ListRow key={r.id} muted title={<span className="font-mono">{r.activityKey}</span>} meta={ruleMeta(r)} />
            ))}
          </List>
        </Section>
      )}

      <Section title="Players" count={links.data?.links.length ?? 0} actions={<Button onClick={() => { setLinking(true); link.reset(); }}>Link a player</Button>}>
        {links.data?.links.length ? (
          <List aria-label="Players">
            {links.data.links.map((l) => {
              const child = childOf(l.childId);
              const name = child?.name ?? 'A child';
              return (
                <ListRow
                  key={l.id}
                  stackOnNarrow
                  leading={child ? <ProfileAvatar name={child.name} color={child.color} size="small" aria-hidden="true" /> : undefined}
                  title={`${name} is ${l.externalId}`}
                  meta={`${providerNames[l.provider] ?? l.provider} · linked ${dateLabel(l.linkedAt)}`}
                  trailing={
                    <Button variant="quiet" aria-label={`Unlink ${l.externalId} from ${name}`} onClick={() => { setUnlinking(l); }}>
                      Unlink…
                    </Button>
                  }
                />
              );
            })}
          </List>
        ) : (
          links.isSuccess && <EmptyState variant="surface" icon={Link2} title="No players linked" description="Link a child’s player ID so their learning earns points." />
        )}
      </Section>

      <Sheet
        open={editing !== null}
        onClose={() => { setEditing(null); }}
        title={editing?.id ? 'Edit rule' : 'Add a rule'}
        description="The activity key is what the learning service sends when a child does it."
        footer={
          <>
            <Button onClick={() => { setEditing(null); }}>Cancel</Button>
            {archivedMeanwhile ? (
              <Button variant="primary" onClick={() => { openRule(editing ? { activityKey: editing.activityKey, points: editing.points, dailyLimit: editing.dailyLimit } : emptyRule); }}>
                Add a rule
              </Button>
            ) : (
              <Button variant="primary" type="submit" form="award-rule" disabled={pending}>
                Save rule
              </Button>
            )}
          </>
        }
      >
        {editing && (
          <form id="award-rule" onSubmit={saveRule} className="grid gap-5" aria-label={editing.id ? 'Edit rule' : 'Add a rule'}>
            {ruleError && !ruleKeyError && <Notice tone={archivedMeanwhile ? 'attention' : 'error'}>{ruleError}</Notice>}
            <Input
              label="Activity key"
              value={editing.activityKey}
              onChange={(e) => { setEditing({ ...editing, activityKey: e.target.value }); }}
              maxLength={160}
              className="font-mono"
              error={ruleKeyError}
              required
              disabled={archivedMeanwhile}
            />
            <div className="grid grid-cols-2 gap-4">
              <Input label="Points each time" type="number" inputMode="numeric" min={1} max={1000000} value={editing.points}
                onChange={(e) => { setEditing({ ...editing, points: e.target.value }); }} required disabled={archivedMeanwhile} />
              <Input label="Daily limit, in points" type="number" inputMode="numeric" min={1} max={1000000} value={editing.dailyLimit} hint="Per child, per day."
                onChange={(e) => { setEditing({ ...editing, dailyLimit: e.target.value }); }} required disabled={archivedMeanwhile} />
            </div>
          </form>
        )}
      </Sheet>

      <ConfirmDialog
        open={archiving !== null}
        title={archiving ? `Archive ${archiving.activityKey}?` : ''}
        description="Services stop giving points for it. Points already given stay."
        confirmLabel={archiving ? `Archive ${archiving.activityKey}` : 'Archive'}
        tone="danger"
        pending={updateRule.isPending}
        error={archiving && updateRule.error ? (errorText[errorCode(updateRule.error) ?? ''] ?? errorMessage(updateRule.error)) : undefined}
        onCancel={() => { setArchiving(null); updateRule.reset(); }}
        onConfirm={() => {
          if (!archiving) return;
          const r = archiving;
          updateRule.mutate(
            {
              params: { ...idempotencyKey(), path: { ruleId: r.id } },
              body: { activityKey: r.activityKey, points: r.points, dailyLimit: r.dailyLimit, archived: true, expectedVersion: r.version },
            },
            { onSuccess: () => { setArchiving(null); }, onError: () => { void refreshRules(); } },
          );
        }}
      />

      <Sheet
        open={linking}
        onClose={() => { setLinking(false); }}
        title="Link a player"
        description="Points for this player go to the child you pick."
        footer={
          <>
            <Button onClick={() => { setLinking(false); }}>Cancel</Button>
            <Button variant="primary" type="submit" form="learner-link" disabled={link.isPending || !linkChild}>
              Link
            </Button>
          </>
        }
      >
        <form id="learner-link" onSubmit={saveLink} className="grid gap-5" aria-label="Link a player">
          <ChoiceGroup
            label="Child"
            onChoose={setLinkChild}
            choices={liveKids.map((c) => ({ value: c.id, label: c.name, variant: c.id === linkChild ? 'primary' : 'secondary' }))}
          />
          <Input label={`${providerNames[PROVIDER] ?? 'Player'} player ID`} value={player} onChange={(e) => { setPlayer(e.target.value); }} maxLength={255} error={linkError} required />
        </form>
      </Sheet>

      <ConfirmDialog
        open={unlinking !== null}
        title={unlinking ? `Unlink ${unlinking.externalId} from ${childOf(unlinking.childId)?.name ?? 'this child'}?` : ''}
        description={`That player stops earning points for ${unlinking ? (childOf(unlinking.childId)?.name ?? 'this child') : ''}. Points already given stay.`}
        confirmLabel={unlinking ? `Unlink ${unlinking.externalId}` : 'Unlink'}
        tone="danger"
        pending={unlink.isPending}
        error={unlink.error ? errorMessage(unlink.error) : undefined}
        onCancel={() => { setUnlinking(null); unlink.reset(); }}
        onConfirm={() => {
          if (!unlinking) return;
          unlink.mutate({ params: { ...idempotencyKey(), path: { linkId: unlinking.id } } }, { onSuccess: () => { setUnlinking(null); } });
        }}
      />
    </>
  );
}
