// @dsCard group="Layout"
import { Plus } from 'lucide-react';

import { Button, PageHeader } from '../../src';

export function Default() {
  return (
    <PageHeader
      title="Rewards"
      description="What points can buy, and the rewards children have chosen."
      actions={
        <Button variant="primary">
          <Plus aria-hidden="true" size={20} />
          Add reward
        </Button>
      }
    />
  );
}

export function TitleOnly() {
  return <PageHeader title="History" />;
}
