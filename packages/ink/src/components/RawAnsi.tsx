import React from 'react';

type Props = {
  /** Pre-rendered ANSI rows. */
  lines: string[];
  /** Column width the producer wrapped `lines` to. */
  width: number;
};

export function RawAnsi({ lines, width }: Props) {
  if (lines.length === 0) return null;

  // `ink-raw-ansi` is a renderer-recognised host element; the reconciler
  // routes it through the dedicated raw-ANSI measure path.
  return (
    <ink-raw-ansi rawText={lines.join('\n')} rawWidth={width} rawHeight={lines.length} />
  );
}
