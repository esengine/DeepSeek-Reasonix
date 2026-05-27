import React, { type PropsWithChildren, type Ref } from 'react';
import type { Except } from 'type-fest';
import type { DOMElement } from '../dom.js';
import type { ClickEvent } from '../events/click-event.js';
import type { FocusEvent } from '../events/focus-event.js';
import type { KeyboardEvent } from '../events/keyboard-event.js';
import type { Styles } from '../styles.js';
import * as warn from '../warn.js';

export type Props = Except<Styles, 'textWrap'> & {
  ref?: Ref<DOMElement>;
  /** Position in the Tab/Shift+Tab cycle. */
  tabIndex?: number;
  /** Receive focus at mount, like the HTML `autofocus` attribute. */
  autoFocus?: boolean;
  onClick?: (event: ClickEvent) => void;
  onFocus?: (event: FocusEvent) => void;
  onFocusCapture?: (event: FocusEvent) => void;
  onBlur?: (event: FocusEvent) => void;
  onBlurCapture?: (event: FocusEvent) => void;
  onKeyDown?: (event: KeyboardEvent) => void;
  onKeyDownCapture?: (event: KeyboardEvent) => void;
  /** Fired when the pointer enters this Box's rendered rect. */
  onMouseEnter?: () => void;
  /** Fired when the pointer leaves this Box's rendered rect. */
  onMouseLeave?: () => void;
};

const INTEGER_SPACING_KEYS = [
  'margin',
  'marginX',
  'marginY',
  'marginTop',
  'marginBottom',
  'marginLeft',
  'marginRight',
  'padding',
  'paddingX',
  'paddingY',
  'paddingTop',
  'paddingBottom',
  'paddingLeft',
  'paddingRight',
  'gap',
  'columnGap',
  'rowGap',
] as const;

/** The fundamental flexbox primitive. */
function Box({
  children,
  flexWrap = 'nowrap',
  flexDirection = 'row',
  flexGrow = 0,
  flexShrink = 1,
  ref,
  tabIndex,
  autoFocus,
  onClick,
  onFocus,
  onFocusCapture,
  onBlur,
  onBlurCapture,
  onMouseEnter,
  onMouseLeave,
  onKeyDown,
  onKeyDownCapture,
  ...style
}: PropsWithChildren<Props>) {
  for (const key of INTEGER_SPACING_KEYS) {
    warn.ifNotInteger((style as Styles)[key] as number | undefined, key);
  }

  const resolvedStyle: Styles = {
    flexWrap,
    flexDirection,
    flexGrow,
    flexShrink,
    ...style,
    overflowX: style.overflowX ?? style.overflow ?? 'visible',
    overflowY: style.overflowY ?? style.overflow ?? 'visible',
  };

  return (
    <ink-box
      ref={ref}
      tabIndex={tabIndex}
      autoFocus={autoFocus}
      onClick={onClick}
      onFocus={onFocus}
      onFocusCapture={onFocusCapture}
      onBlur={onBlur}
      onBlurCapture={onBlurCapture}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      onKeyDown={onKeyDown}
      onKeyDownCapture={onKeyDownCapture}
      style={resolvedStyle}
    >
      {children}
    </ink-box>
  );
}

export default Box;
