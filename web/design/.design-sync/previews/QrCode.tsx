// @dsCard group="Screen time"
import { QrCode } from '../../src';
import { pairingModules } from './fixtures';

export function Pairing() {
  return (
    <div className="max-w-60">
      <QrCode modules={pairingModules} label="Pairing code for Abby’s iPad" />
    </div>
  );
}
