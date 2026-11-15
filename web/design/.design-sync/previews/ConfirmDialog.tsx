// @dsCard group="Overlays"
import { ConfirmDialog } from '../../src';

export function Archive() {
  return (
    <ConfirmDialog
      open
      tone="danger"
      title="Archive Tablet time?"
      description="Children won’t see it any more. History keeps every time it was chosen."
      confirmLabel="Archive"
      onConfirm={() => undefined}
      onCancel={() => undefined}
    />
  );
}
