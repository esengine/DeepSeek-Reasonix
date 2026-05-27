import React, { type Ref, useCallback, useEffect, useRef, useState } from 'react';
import type { Except } from 'type-fest';
import type { DOMElement } from '../dom.js';
import type { ClickEvent } from '../events/click-event.js';
import type { FocusEvent } from '../events/focus-event.js';
import type { KeyboardEvent } from '../events/keyboard-event.js';
import type { Styles } from '../styles.js';
import Box from './Box.js';

/** How long the visual `active` flag stays true after a press. */
const ACTIVE_FLASH_MS = 100;

type ButtonState = {
  focused: boolean;
  hovered: boolean;
  active: boolean;
};

export type Props = Except<Styles, 'textWrap'> & {
  ref?: Ref<DOMElement>;
  onAction: () => void;
  /** Tab order index. */
  tabIndex?: number;
  /** Focus the button at mount. */
  autoFocus?: boolean;
  children: ((state: ButtonState) => React.ReactNode) | React.ReactNode;
};

function Button({
  onAction,
  tabIndex = 0,
  autoFocus,
  children,
  ref,
  ...style
}: Props) {
  const [isFocused, setIsFocused] = useState(false);
  const [isHovered, setIsHovered] = useState(false);
  const [isActive, setIsActive] = useState(false);
  const activeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (activeTimer.current) clearTimeout(activeTimer.current);
    },
    [],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'return' || e.key === ' ') {
        e.preventDefault();
        setIsActive(true);
        onAction();
        if (activeTimer.current) clearTimeout(activeTimer.current);
        activeTimer.current = setTimeout(() => setIsActive(false), ACTIVE_FLASH_MS);
      }
    },
    [onAction],
  );

  const handleClick = useCallback(
    (_e: ClickEvent) => {
      onAction();
    },
    [onAction],
  );

  const handleFocus = useCallback((_e: FocusEvent) => setIsFocused(true), []);
  const handleBlur = useCallback((_e: FocusEvent) => setIsFocused(false), []);
  const handleMouseEnter = useCallback(() => setIsHovered(true), []);
  const handleMouseLeave = useCallback(() => setIsHovered(false), []);

  const state: ButtonState = { focused: isFocused, hovered: isHovered, active: isActive };
  const content = typeof children === 'function' ? children(state) : children;

  return (
    <Box
      ref={ref}
      tabIndex={tabIndex}
      autoFocus={autoFocus}
      onKeyDown={handleKeyDown}
      onClick={handleClick}
      onFocus={handleFocus}
      onBlur={handleBlur}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      {...style}
    >
      {content}
    </Box>
  );
}

export default Button;
export type { ButtonState };
