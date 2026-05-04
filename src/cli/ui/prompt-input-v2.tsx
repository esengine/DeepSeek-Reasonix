/** Single-line prompt input for chat-v2 — useCursor + useKeystroke, no Ink. */

// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import stringWidth from "string-width";
import { inkCompat, useCursor, useKeystroke } from "../../renderer/index.js";

const FG_BODY = "#c9d1d9";
const FG_FAINT = "#6e7681";
const TONE_BRAND = "#79c0ff";

export interface SimplePromptInputProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly onSubmit?: (value: string) => void;
  readonly onCancel?: () => void;
  /** Hint shown when value is empty. */
  readonly placeholder?: string;
  /** Leading marker before the input. Default `›`. */
  readonly prefix?: string;
  /** Disable input; cursor still renders. */
  readonly disabled?: boolean;
}

/** Cursor index is held internally — caller controls the value, but the
 *  caret position is a UI concern. Reset to value.length whenever the value
 *  is replaced from outside (longer or shorter than current cursor allows). */
export function SimplePromptInput({
  value,
  onChange,
  onSubmit,
  onCancel,
  placeholder,
  prefix = "›",
  disabled,
}: SimplePromptInputProps): React.ReactElement {
  const [cursor, setCursor] = React.useState(value.length);

  // External value replacement: clamp / reset cursor.
  const lastValueRef = React.useRef(value);
  if (value !== lastValueRef.current) {
    lastValueRef.current = value;
    if (cursor > value.length) setCursor(value.length);
  }

  // Multiple keystrokes can flush in one stdin chunk before React commits;
  // hold the latest value/cursor in refs so the handler reads them fresh.
  const valueRef = React.useRef(value);
  valueRef.current = value;
  const cursorRef = React.useRef(cursor);
  cursorRef.current = cursor;

  const apply = (next: string, nextCursor: number): void => {
    valueRef.current = next;
    cursorRef.current = nextCursor;
    setCursor(nextCursor);
    onChange(next);
  };

  useKeystroke((k) => {
    if (disabled) return;
    if (k.escape) {
      if (valueRef.current.length === 0) {
        onCancel?.();
        return;
      }
      apply("", 0);
      return;
    }
    if (k.return) {
      onSubmit?.(valueRef.current);
      return;
    }
    const v = valueRef.current;
    const c = cursorRef.current;
    if (k.backspace) {
      if (c === 0) return;
      apply(v.slice(0, c - 1) + v.slice(c), c - 1);
      return;
    }
    if (k.delete) {
      if (c >= v.length) return;
      apply(v.slice(0, c) + v.slice(c + 1), c);
      return;
    }
    if (k.leftArrow) {
      if (c === 0) return;
      cursorRef.current = c - 1;
      setCursor(c - 1);
      return;
    }
    if (k.rightArrow) {
      if (c >= v.length) return;
      cursorRef.current = c + 1;
      setCursor(c + 1);
      return;
    }
    if (k.home || (k.ctrl && k.input === "a")) {
      cursorRef.current = 0;
      setCursor(0);
      return;
    }
    if (k.end || (k.ctrl && k.input === "e")) {
      cursorRef.current = v.length;
      setCursor(v.length);
      return;
    }
    if (k.ctrl && k.input === "u") {
      // Bash convention: kill from cursor to start of line.
      apply(v.slice(c), 0);
      return;
    }
    if (k.ctrl && k.input === "k") {
      apply(v.slice(0, c), c);
      return;
    }
    if (k.ctrl || k.meta) return;
    if (k.input.length > 0) {
      apply(v.slice(0, c) + k.input + v.slice(c), c + k.input.length);
    }
  });

  const prefixCells = stringCells(prefix) + 1; // prefix + 1 space
  const visualCol = prefixCells + stringCells(value.slice(0, cursor));
  useCursor(disabled ? null : { col: visualCol, rowFromBottom: 0, visible: true });

  const showPlaceholder = value.length === 0;
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={TONE_BRAND} bold>
        {prefix}
      </inkCompat.Text>
      {showPlaceholder ? (
        <inkCompat.Text dimColor color={FG_FAINT}>
          {placeholder ?? "type a message…"}
        </inkCompat.Text>
      ) : (
        <inkCompat.Text color={FG_BODY}>{value}</inkCompat.Text>
      )}
    </inkCompat.Box>
  );
}

function stringCells(s: string): number {
  if (s.length === 0) return 0;
  return stringWidth(s);
}
