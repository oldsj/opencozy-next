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
      maxLength={length}
      minLength={length}
      pattern={`[A-Za-z0-9]{${String(length)}}`}
      className={cx('oc-input--code', className)}
    />
  );
}
