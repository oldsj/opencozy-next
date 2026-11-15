// @dsCard group="Feedback"
import { Button, ConnectionBanner } from '../../src';

export function Stale() {
  return (
    <ConnectionBanner
      status="stale"
      lastUpdated="4:12 pm"
      actions={
        <Button variant="secondary">
          Try again
        </Button>
      }
    />
  );
}

export function Offline() {
  return <ConnectionBanner status="offline" lastUpdated="4:12 pm" />;
}
