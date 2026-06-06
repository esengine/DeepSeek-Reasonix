import React from 'react';

export type Props = {
  /** How many `\n` characters to emit. */
  readonly count?: number;
};

/** Emits one or more newline characters inside a `<Text>` block. */
export default function Newline({ count = 1 }: Props) {
  return <ink-text>{'\n'.repeat(count)}</ink-text>;
}
