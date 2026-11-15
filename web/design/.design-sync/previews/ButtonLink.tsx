// @dsCard group="Actions"
import { ButtonLink } from '../../src';

// A plain anchor; an app wraps it for its router (TanStack: createLink(ButtonLink)).
export function Default() {
  return <ButtonLink href="#sign-in">Adult sign-in</ButtonLink>;
}

export function Primary() {
  return (
    <ButtonLink variant="primary" href="#rewards">
      See rewards
    </ButtonLink>
  );
}
