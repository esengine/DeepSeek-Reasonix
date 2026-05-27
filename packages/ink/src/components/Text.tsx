import React, { type ReactNode } from 'react';
import type { Color, Styles, TextStyles } from '../styles.js';

type BaseProps = {
  /** Foreground colour. */
  readonly color?: Color;

  /** Background colour. Same accepted forms as `color`. */
  readonly backgroundColor?: Color;

  /** Render italic. */
  readonly italic?: boolean;

  /** Render underlined. */
  readonly underline?: boolean;

  /** Render with a strikethrough line. */
  readonly strikethrough?: boolean;

  /** Swap foreground and background. */
  readonly inverse?: boolean;

  /** How to handle text that exceeds the parent's content width. */
  readonly wrap?: Styles['textWrap'];
  readonly children?: ReactNode;
  readonly bold?: boolean;
  readonly dim?: boolean;
};

export type Props = BaseProps;

/** Pre-built `Styles` objects keyed by `textWrap` value. */
const STYLES_BY_WRAP: Record<NonNullable<Styles['textWrap']>, Styles> = {
  wrap: { flexGrow: 0, flexShrink: 1, flexDirection: 'row', textWrap: 'wrap' },
  'wrap-trim': { flexGrow: 0, flexShrink: 1, flexDirection: 'row', textWrap: 'wrap-trim' },
  end: { flexGrow: 0, flexShrink: 1, flexDirection: 'row', textWrap: 'end' },
  middle: { flexGrow: 0, flexShrink: 1, flexDirection: 'row', textWrap: 'middle' },
  'truncate-end': { flexGrow: 0, flexShrink: 1, flexDirection: 'row', textWrap: 'truncate-end' },
  truncate: { flexGrow: 0, flexShrink: 1, flexDirection: 'row', textWrap: 'truncate' },
  'truncate-middle': {
    flexGrow: 0,
    flexShrink: 1,
    flexDirection: 'row',
    textWrap: 'truncate-middle',
  },
  'truncate-start': {
    flexGrow: 0,
    flexShrink: 1,
    flexDirection: 'row',
    textWrap: 'truncate-start',
  },
};

/** Renders a run of styled text. */
export default function Text({
  color,
  backgroundColor,
  bold,
  dim,
  italic = false,
  underline = false,
  strikethrough = false,
  inverse = false,
  wrap = 'wrap',
  children,
}: Props) {
  if (children === undefined || children === null) return null;

  const textStyles: TextStyles = {
    ...(color && { color }),
    ...(backgroundColor && { backgroundColor }),
    ...(dim && { dim }),
    ...(bold && { bold }),
    ...(italic && { italic }),
    ...(underline && { underline }),
    ...(strikethrough && { strikethrough }),
    ...(inverse && { inverse }),
  };

  return (
    <ink-text style={STYLES_BY_WRAP[wrap]} textStyles={textStyles}>
      {children}
    </ink-text>
  );
}
