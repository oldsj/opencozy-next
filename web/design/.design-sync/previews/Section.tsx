// @dsCard group="Layout"
import { List, ListRow, Section, SegmentedControl } from '../../src';

export function WithFilter() {
  return (
    <Section
      title="Catalog"
      count={3}
      actions={
        <SegmentedControl
          label="Which rewards"
          value="active"
          onChange={() => undefined}
          options={[
            { value: 'active', label: 'Available' },
            { value: 'archived', label: 'Archived' },
          ]}
        />
      }
    >
      <List>
        <ListRow title="Extra bedtime story" meta="15 points" />
        <ListRow title="Tablet time" meta="20 points · 30 min iPad time" />
        <ListRow title="Trip to the ice-cream shop" meta="80 points" />
      </List>
    </Section>
  );
}

export function WithDescription() {
  return (
    <Section title="Waiting for you" description="Rewards children have chosen. Hand them over, then mark them as given.">
      <p className="text-muted-foreground">Nothing is waiting.</p>
    </Section>
  );
}
