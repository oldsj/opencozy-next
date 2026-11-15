// @dsCard group="Layout"
import { Button, List, ListRow, ProfileAvatar, Tag } from '../../src';

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
