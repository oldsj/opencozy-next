// @dsCard group="Actions"
import { ChoiceGroup } from '../../src';

const noop = () => undefined;

export function WhosWatching() {
  return (
    <ChoiceGroup
      label="Who’s watching?"
      onChoose={noop}
      choices={[
        { value: 'parents', label: 'Parents', variant: 'primary' },
        { value: 'ada', label: 'Ada', detail: '25 min' },
        { value: 'ezra', label: 'Ezra', detail: 'no time', disabled: true },
        { value: 'ivy', label: 'Ivy', detail: 'from 4:00 pm', disabled: true },
        { value: 'open', label: 'Open 1 hour' },
        { value: 'not_now', label: 'Not now', variant: 'quiet' },
      ]}
    />
  );
}

export function MoreTime() {
  return (
    <ChoiceGroup
      label="More iPad time for Noor"
      onChoose={noop}
      choices={[
        { value: '30min', label: '30 min' },
        { value: '1h', label: '1 hour' },
        { value: 'all_day', label: 'All day' },
        { value: 'no', label: 'No', variant: 'quiet' },
      ]}
    />
  );
}

export function Sending() {
  return (
    <ChoiceGroup
      label="More iPad time for Noor"
      onChoose={noop}
      busy
      choices={[
        { value: '30min', label: '30 min' },
        { value: '1h', label: '1 hour' },
        { value: 'no', label: 'No', variant: 'quiet' },
      ]}
    />
  );
}
