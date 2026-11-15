// @dsCard group="Forms"
import { Button, CodeInput } from '../../src';

export function Pairing() {
  return (
    <form className="grid max-w-md gap-4">
      <CodeInput label="Six-character family code" hint="It expires after five minutes and works once." />
      <div>
        <Button type="submit" variant="primary">
          Open family dashboard
        </Button>
      </div>
    </form>
  );
}

export function Invalid() {
  return <CodeInput label="Six-character family code" defaultValue="K7Q2" error="That code has expired. Ask an adult for a new one." />;
}
