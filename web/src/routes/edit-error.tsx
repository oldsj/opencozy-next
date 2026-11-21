import { Notice } from '@app/design';

import { errorCode, errorMessage } from '../api/client';

/**
 * A failed save. A stale one (version_conflict) is a note, not an error: the form already shows
 * the newer version, to check and save again.
 */
export function EditError({ error }: { error: unknown }) {
  if (error === null || error === undefined) return null;
  return <Notice tone={errorCode(error) === 'version_conflict' ? 'attention' : 'error'}>{errorMessage(error)}</Notice>;
}
