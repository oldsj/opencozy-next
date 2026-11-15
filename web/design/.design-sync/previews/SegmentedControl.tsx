// @dsCard group="Forms"
import { useState } from 'react';

import { SegmentedControl } from '../../src';

export function Default() {
  const [value, setValue] = useState('all');
  return (
    <SegmentedControl
      label="Show history for"
      value={value}
      onChange={setValue}
      options={[
        { value: 'all', label: 'Everyone' },
        { value: 'ada', label: 'Ada' },
        { value: 'ezra', label: 'Ezra' },
        { value: 'ivy', label: 'Ivy' },
      ]}
    />
  );
}
