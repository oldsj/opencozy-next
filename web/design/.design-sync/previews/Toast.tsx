// @dsCard group="Feedback"
import { Toast } from '../../src';

// The toast is fixed to the viewport corner; the empty box only gives the preview some height.
export function Confirmed() {
  return (
    <div className="h-40">
      <Toast>Nice, Ada! +5 points</Toast>
    </div>
  );
}
