// @dsCard group="Layout"
import { Button, List, ListRow, ManageNav, ManageShell, PageHeader, Section } from '../../src';
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
