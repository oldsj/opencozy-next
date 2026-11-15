// @dsCard group="Layout"
import { List, ListRow, ProfileAvatar, Tag } from '../../src';

export function Person() {
  return (
    <List>
      <ListRow
        leading={<ProfileAvatar name="Noor" color="lilac" size="small" aria-hidden="true" />}
        title="Noor"
        meta="iPad · allowed 3 PM–7 PM"
        trailing={<Tag tone="positive">Paired</Tag>}
      />
    </List>
  );
}

export function Muted() {
  return (
    <List>
      <ListRow muted title="Old kitchen display" meta="Last seen Aug 2" />
    </List>
  );
}
