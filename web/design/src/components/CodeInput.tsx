import type { InputProps } from './Input';
import { Input } from './Input';
import { cx } from '../cx';

export interface CodeInputProps extends Omit<InputProps, 'type' | 'maxLength' | 'minLength' | 'pattern'> {
  /** Characters in the code; the pairing code has six. */
  length?: number;
}

/**
 * A one-time code (display pairing): large, spaced, letters and digits, no autocomplete, spelling
 * or autocorrect. Pair it with a sentence saying where the code comes from and when it expires.
 */
/**
 * Pairing codes use letters and digits without 0, O, 1, I or L. The server ignores case, spaces
 * and dashes, so the field accepts "k7q-2xm" as well as "K7Q2XM".
 */
const CODE_CHAR = '[A-HJKMNP-Za-hjkmnp-z2-9]';

function codePattern(length: number): string {
  return `[ \\-]*(?:${CODE_CHAR}[ \\-]*){${String(length)}}`;
}

export function CodeInput({ className, length = 6, ...props }: CodeInputProps) {
  return (
    <Input
      autoCapitalize="characters"
      autoComplete="off"
      autoCorrect="off"
      spellCheck={false}
      inputMode="text"
      enterKeyHint="go"
      {...props}
      type="text"
      maxLength={length * 2}
      minLength={length}
      pattern={codePattern(length)}
      className={cx('oc-input--code', className)}
    />
  );
}
