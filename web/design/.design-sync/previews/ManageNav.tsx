// @dsCard group="Layout"
import { Button, ManageNav } from '../../src';
import { manageGroups } from './fixtures-manage';

/** Resize the window: a rail from 1024px, a bar with Menu below. */
export function OnChores() {
  return (
    <ManageNav
      groups={manageGroups('/manage/chores')}
      shortcut="/manage/inbox"
      back={{ href: '/today', label: 'Back to family' }}
      footer={<Button variant="quiet">Sign out</Button>}
    />
  );
}

export function OnInbox() {
  return <ManageNav groups={manageGroups('/manage/inbox', 0)} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />;
}
