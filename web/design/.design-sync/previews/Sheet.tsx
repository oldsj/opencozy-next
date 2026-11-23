// @dsCard group="Overlays"
import { Button, Sheet } from '../../src';

export function Default() {
  return (
    <Sheet
      open
      onClose={() => undefined}
      title="Choose Movie night?"
      description="This uses 50 points from Abby."
      footer={
        <>
          <Button>Not now</Button>
          <Button variant="primary">Use 50 points</Button>
        </>
      }
    >
      <p>Abby will have 18 points left.</p>
    </Sheet>
  );
}
