// @dsCard group="Layout"
import { Button, ChoiceGroup, List, ListRow, ProfileAvatar, Section, Tag } from '../../src';

const noop = () => undefined;

export function WithActions() {
  return (
    <List>
      <ListRow
        stackOnNarrow
        leading={<ProfileAvatar name="Ezra" color="sage" size="small" aria-hidden="true" />}
        title="Extra bedtime story"
        meta="Ezra · 15 points · Today, 12:29 PM"
        trailing={
          <>
            <Button variant="danger">Cancel and refund</Button>
            <Button variant="primary">Mark as given</Button>
          </>
        }
      />
      <ListRow
        title="Tablet time"
        meta="20 points · 30 min iPad time"
        trailing={
          <Button variant="quiet" aria-label="Edit Tablet time">
            Edit
          </Button>
        }
      />
    </List>
  );
}

export function History() {
  return (
    <List>
      <ListRow leading={<ProfileAvatar name="Ivy" color="rose" size="small" aria-hidden="true" />} title="Pick dinner on Friday" meta="Ivy · 30 points · Yesterday, 6:10 PM" />
      <ListRow
        leading={<ProfileAvatar name="Ezra" color="sage" size="small" aria-hidden="true" />}
        title="Stay up 20 minutes later"
        meta="Ezra · 25 points · Wed, Nov 4"
        trailing={<Tag>Canceled · 25 back</Tag>}
      />
      <ListRow muted title="Movie night pick" meta="35 points · archived Sep 11" />
    </List>
  );
}

/** The approvals inbox: a TV prompt, a time request, then completions oldest first. */
export function Inbox() {
  return (
    <div className="grid gap-6">
      <Section title="Who’s watching?" count={1} description="This TV is on but stays offline until you choose.">
        <List>
          <ListRow title="Living room TV" meta="Turned on · 2 minutes ago">
            <ChoiceGroup
              className="mt-3"
              label="Who’s watching the living room TV?"
              onChoose={noop}
              choices={[
                { value: 'parents', label: 'Parents', variant: 'primary' },
                { value: 'ada', label: 'Ada', detail: '25 min' },
                { value: 'ezra', label: 'Ezra', detail: 'no time', disabled: true },
                { value: 'not_now', label: 'Not now', variant: 'quiet' },
              ]}
            />
          </ListRow>
        </List>
      </Section>
      <Section title="Asking for more time" count={1} description="Time you give starts on their iPad right away.">
        <List>
          <ListRow
            stackOnNarrow
            title="Noor wants more iPad time"
            meta="Asked 5 minutes ago"
            trailing={
              <ChoiceGroup
                label="More iPad time for Noor"
                onChoose={noop}
                choices={[
                  { value: '30min', label: '30 min' },
                  { value: '1h', label: '1 hour' },
                  { value: 'all_day', label: 'All day' },
                  { value: 'no', label: 'No', variant: 'quiet' },
                ]}
              />
            }
          />
        </List>
      </Section>
      <Section title="Waiting for approval" count={2} description="Oldest first.">
        <List>
          <ListRow
            stackOnNarrow
            leading={<ProfileAvatar name="Ada" color="sky" size="small" aria-hidden="true" />}
            title="Tidy the hallway"
            meta="Ada · 5 points · Chore · 9:40 AM"
            trailing={
              <>
                <Button variant="danger">Reject…</Button>
                <Button variant="primary">Approve</Button>
              </>
            }
          />
          <ListRow
            stackOnNarrow
            leading={<ProfileAvatar name="Ezra" color="sage" size="small" aria-hidden="true" />}
            title="Morning"
            meta="Ezra · 3 points · Routine · 7:55 AM"
            trailing={
              <>
                <Button variant="danger" disabled>
                  Reject…
                </Button>
                <Button variant="primary" disabled>
                  Saving…
                </Button>
              </>
            }
          />
        </List>
      </Section>
    </div>
  );
}

/** Rewards children chose (hand over, or cancel and refund) and what happened to earlier ones. */
export function Redemptions() {
  return (
    <div className="grid gap-6">
      <Section title="Waiting for you" count={1} description="Hand it over, then mark it as given.">
        <List>
          <ListRow
            stackOnNarrow
            leading={<ProfileAvatar name="Ivy" color="rose" size="small" aria-hidden="true" />}
            title="Pick the movie"
            meta="Ivy · 40 points · Yesterday, 6:10 PM"
            trailing={
              <>
                <Button variant="danger">Cancel and refund</Button>
                <Button variant="primary">Mark as given</Button>
              </>
            }
          />
        </List>
      </Section>
      <Section title="Banked screen time">
        <List>
          <ListRow
            stackOnNarrow
            title="Ada · 45 min TV time"
            meta="From “Movie afternoon” · spent only while watching"
            trailing={<Button variant="danger">Cancel unused time</Button>}
          />
        </List>
      </Section>
      <Section title="Recently">
        <List>
          <ListRow muted title="Extra bedtime story" meta="Ezra · 15 points · Given · Monday" trailing={<Tag tone="positive">Given</Tag>} />
          <ListRow muted title="Bake cookies" meta="Noor · 30 points · Sunday" trailing={<Tag>Canceled · 30 back</Tag>} />
        </List>
      </Section>
    </div>
  );
}
