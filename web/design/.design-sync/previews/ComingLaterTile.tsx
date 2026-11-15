// @dsCard group="Feedback"
import { CloudSun, Video } from 'lucide-react';

import { ComingLaterTile } from '../../src';

export function Default() {
  return (
    <div className="grid gap-3">
      <ComingLaterTile icon={CloudSun} title="Weather" description="Forecasts will appear here." />
      <ComingLaterTile icon={Video} title="Family video" />
    </div>
  );
}
