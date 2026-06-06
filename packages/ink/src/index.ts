import {
  createElement,
  Fragment,
  type ReactElement,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react';
import { Ansi } from './Ansi.js';
import Box from './components/Box.js';
import { TerminalSizeContext } from './components/TerminalSizeContext.js';
import Text from './components/Text.js';
import type { DOMElement } from './dom.js';
import { useAnimationFrame } from './hooks/use-animation-frame.js';
import useApp from './hooks/use-app.js';
import useInput from './hooks/use-input.js';
import { useAnimationTimer, useInterval } from './hooks/use-interval.js';
import { useSelection } from './hooks/use-selection.js';
import useStdin from './hooks/use-stdin.js';
import { useTabStatus } from './hooks/use-tab-status.js';
import { useTerminalFocus } from './hooks/use-terminal-focus.js';
import { useTerminalTitle } from './hooks/use-terminal-title.js';
import { useTerminalViewport } from './hooks/use-terminal-viewport.js';
import measureElement from './measure-element.js';

export { Box, Text, Ansi };
export { default as Newline } from './components/Newline.js';
export { default as Spacer } from './components/Spacer.js';
export { default as Link } from './components/Link.js';
export { default as Button } from './components/Button.js';
export { AlternateScreen } from './components/AlternateScreen.js';
export { NoSelect } from './components/NoSelect.js';
export { RawAnsi } from './components/RawAnsi.js';
export { default as ScrollBox } from './components/ScrollBox.js';
export type { ScrollBoxHandle, ScrollBoxProps } from './components/ScrollBox.js';
export { default as TextInput, type TextInputProps } from './components/TextInput.js';

export type { Props as BoxProps } from './components/Box.js';
export type { Props as TextProps } from './components/Text.js';

export {
  useApp,
  useInput,
  useStdin,
  useAnimationFrame,
  useAnimationTimer,
  useInterval,
  useSelection,
  useTabStatus,
  useTerminalFocus,
  useTerminalTitle,
  useTerminalViewport,
  measureElement,
};

export type { DOMElement };
export { ClickEvent } from './events/click-event.js';
export { EventEmitter } from './events/emitter.js';
export { Event } from './events/event.js';
export { InputEvent, type Key } from './events/input-event.js';
export { TerminalFocusEvent } from './events/terminal-focus-event.js';
export { FocusManager } from './focus.js';
export { stringWidth } from './stringWidth.js';
export { default as wrapText } from './wrap-text.js';
export type {
  AnsiColor,
  Ansi256Color,
  Color,
  HexColor,
  RGBColor,
  Styles,
  TextStyles,
} from './styles.js';

export { getLastInteractionTime } from './_internal/state.js';

export {
  renderSync as render,
  default as renderAsync,
  createRoot,
  renderSync,
  type Instance,
  type RenderOptions,
  type Root,
} from './root.js';

export function useStdout(): {
  stdout: NodeJS.WriteStream;
  write: (data: string) => void;
} {
  const { stdout } = useStdin() as unknown as { stdout: NodeJS.WriteStream };
  const real = stdout ?? process.stdout;
  return {
    stdout: real,
    write: (data: string) => real.write(data),
  };
}

export function useTerminalSize(): { columns: number; rows: number } {
  const ctx = useContext(TerminalSizeContext);
  if (ctx) return { columns: ctx.columns, rows: ctx.rows };
  return {
    columns: process.stdout.columns || 80,
    rows: process.stdout.rows || 24,
  };
}

type StaticProps<T> = {
  items: readonly T[];
  children: (item: T, index: number) => ReactNode;
  style?: unknown;
};

/** Append-only list. Memo'd children skip re-render when their item reference is unchanged. */
export function Static<T>({ items, children }: StaticProps<T>): ReactElement {
  return createElement(
    Fragment,
    null,
    items.map((item, i) => children(item, i)),
  );
}

export function useBoxMetrics(ref: { current: DOMElement | null }): {
  width: number;
  height: number;
} {
  const [size, setSize] = useState({ width: 0, height: 0 });
  const scheduledRef = useRef(false);
  const lastMeasureTimeRef = useRef(0);

  // No dep array — this hook must re-measure when the parent layout
  // changes, which can only be detected by re-running on every render.
  // Using `ref.current` as a dependency is a React anti-pattern (refs
  // are not reactive) and would cause stale dimensions after parent
  // resizes.
  //
  // To prevent excessive measurements during rapid scrolling (10-20
  // wheel events/100ms on Windows), we throttle to at most one measure
  // per frame via the deferred setTimeout + scheduledRef guard, and
  // skip re-measurement if less than 16ms have elapsed since the last
  // successful measurement (one frame at 60fps).
  useEffect(() => {
    if (!ref.current) return;
    if (scheduledRef.current) return;

    const now = Date.now();
    if (now - lastMeasureTimeRef.current < 16) return;

    scheduledRef.current = true;
    // Defer measure off the React commit batch — re-measuring
    // synchronously in the same depth-counted chain crashes with
    // "Maximum update depth exceeded" when Box content doesn't
    // converge in one layout pass.
    setTimeout(() => {
      scheduledRef.current = false;
      if (!ref.current) return;
      lastMeasureTimeRef.current = Date.now();
      const { width, height } = measureElement(ref.current);
      setSize((prev) =>
        prev.width === width && prev.height === height ? prev : { width, height },
      );
    }, 0);
  });
  return size;
}
