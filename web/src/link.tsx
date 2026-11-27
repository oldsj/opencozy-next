import { createLink } from '@tanstack/react-router';

import { ButtonLink } from '@app/design';

/** A design-system ButtonLink that navigates in the app: `<RouterButtonLink to="/calendars">`. */
export const RouterButtonLink = createLink(ButtonLink);
