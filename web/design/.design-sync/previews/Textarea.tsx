// @dsCard group="Forms"
import { Textarea } from '../../src';

export function Default() {
  return <Textarea label="Description" hint="Shown to children under the reward’s name." defaultValue="Pick the film for Friday night." />;
}

export function WithError() {
  return <Textarea label="Lock message" defaultValue="" error="Write a short message the child will see." />;
}
