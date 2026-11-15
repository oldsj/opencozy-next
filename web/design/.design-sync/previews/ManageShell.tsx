// @dsCard group="Layout"
import { Button, ChoiceGroup, List, ListRow, ManageNav, ManageShell, PageHeader, ProfileAvatar, Section, Sheet, Textarea } from '../../src';
import { inbox, kindLabels, REJECT_REASON_MAX, type InboxItem } from './fixtures-inbox';
import { manageGroups } from './fixtures-manage';

export function Chores() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/chores')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Chores" description="What each child can do for points." actions={<Button variant="primary">Add chore</Button>} />
      <Section title="Every day">
        <List>
          <ListRow title="Feed the cat" meta="Ezra · 4 points · needs approval" trailing={<Button variant="quiet">Edit</Button>} />
          <ListRow title="Tidy the hallway" meta="Ada, Noor · 5 points" trailing={<Button variant="quiet">Edit</Button>} />
        </List>
      </Section>
    </ManageShell>
  );
}

const noop = () => undefined;

function InboxRow({ item, saving = false }: { item: InboxItem; saving?: boolean }) {
  const meta = [item.child.name, item.points === undefined ? null : `${String(item.points)} points`, kindLabels[item.kind], item.submittedAt]
    .filter(Boolean)
    .join(' · ');
  const leading = <ProfileAvatar name={item.child.name} color={item.child.color} size="small" aria-hidden="true" />;
  if (item.kind === 'time_request') {
    return (
      <ListRow
        stackOnNarrow
        leading={leading}
        title={`${item.child.name} wants more iPad time`}
        meta={meta}
        trailing={
          <ChoiceGroup
            label={`More iPad time for ${item.child.name}`}
            onChoose={noop}
            choices={[
              { value: '30', label: '30 min' },
              { value: '60', label: '1 hour' },
              { value: 'all_day', label: 'All day' },
              { value: 'no', label: 'No', variant: 'quiet' },
            ]}
          />
        }
      />
    );
  }
  return (
    <ListRow
      stackOnNarrow
      leading={leading}
      title={item.title}
      meta={meta}
      trailing={
        <>
          <Button variant="danger" disabled={saving} aria-label={`Send back: ${item.title} (${item.child.name})`}>
            Send back…
          </Button>
          <Button variant="primary" disabled={saving} aria-label={saving ? undefined : `Approve: ${item.title} (${item.child.name})`}>
            {saving ? 'Saving…' : 'Approve'}
          </Button>
        </>
      }
    />
  );
}

/** The adult inbox (phone and desktop), built only from `inbox` (fixtures-inbox.ts). */
export function Inbox() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/inbox')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Inbox" description="Approve what the children finished. Oldest first." />
      <Section title="Waiting for you" count={inbox.items.length}>
        <List>
          {inbox.items.map((item, i) => (
            <InboxRow key={item.id} item={item} saving={i === 0} />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

/** Sending a chore back: the reason is optional, shown to the child, up to 200 characters. */
export function InboxSendBack() {
  return (
    <>
      <Inbox />
      <Sheet
        open
        onClose={noop}
        title="Send back Tidy the hallway?"
        description="Ada keeps the chore and can tick it again. No points move."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="danger">Send back</Button>
          </>
        }
      >
        <Textarea
          label="Reason for Ada (optional)"
          hint={`Shown on Ada’s list. Up to ${String(REJECT_REASON_MAX)} characters.`}
          maxLength={REJECT_REASON_MAX}
          defaultValue="The shoes are tidied, but the coats still need hanging up."
        />
      </Sheet>
    </>
  );
}
