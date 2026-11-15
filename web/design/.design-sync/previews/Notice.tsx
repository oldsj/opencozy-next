// @dsCard group="Feedback"
import { Button, Notice } from '../../src';

export function Tones() {
  return (
    <div className="grid max-w-xl gap-3">
      <Notice tone="info">This display can view routines but cannot check steps yet.</Notice>
      <Notice tone="success">Saved Tablet time (20 points).</Notice>
      <Notice tone="error">The rewards service is unavailable. Try again later.</Notice>
    </div>
  );
}

export function WithActions() {
  return (
    <Notice
      tone="attention"
      title="Was this change saved?"
      actions={
        <>
          <Button variant="primary">Try again</Button>
          <Button>Check reward list</Button>
        </>
      }
    >
      OpenCozy kept the change so it will not be applied twice. Check the list first, then try again if needed.
    </Notice>
  );
}
