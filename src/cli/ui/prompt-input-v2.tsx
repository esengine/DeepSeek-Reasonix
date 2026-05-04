/** Multi-line prompt input for chat-v2 — useCursor + useKeystroke, no Ink. */

// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import stringWidth from "string-width";
import { inkCompat, useCursor, useKeystroke } from "../../renderer/index.js";
import { lineAndColumn, processMultilineKey } from "./multiline-keys.js";

const FG_BODY = "#c9d1d9";
const FG_FAINT = "#6e7681";
const TONE_BRAND = "#79c0ff";

export interface SimplePromptInputProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly onSubmit?: (value: string) => void;
  readonly onCancel?: () => void;
  /** Fired when ↑ pushes past the top of an empty / boundary buffer. Parent walks history. */
  readonly onHistoryPrev?: () => void;
  /** Fired when ↓ pushes past the bottom of an empty / boundary buffer. */
  readonly onHistoryNext?: () => void;
  /** Hint shown when value is empty. */
  readonly placeholder?: string;
  /** Leading marker before the input. Default `›`. */
  readonly prefix?: string;
  /** Disable input; cursor still renders. */
  readonly disabled?: boolean;
}

export function SimplePromptInput({
  value,
  onChange,
  onSubmit,
  onCancel,
  onHistoryPrev,
  onHistoryNext,
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

  const apply = (nextValue: string | null, nextCursor: number | null): void => {
    if (nextValue !== null && nextValue !== valueRef.current) {
      valueRef.current = nextValue;
      onChange(nextValue);
    }
    if (nextCursor !== null && nextCursor !== cursorRef.current) {
      cursorRef.current = nextCursor;
      setCursor(nextCursor);
    }
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
    const action = processMultilineKey(valueRef.current, cursorRef.current, {
      input: k.input,
      return: k.return,
      shift: k.shift,
      ctrl: k.ctrl,
      meta: k.meta,
      backspace: k.backspace,
      delete: k.delete,
      tab: k.tab,
      upArrow: k.upArrow,
      downArrow: k.downArrow,
      leftArrow: k.leftArrow,
      rightArrow: k.rightArrow,
      escape: k.escape,
      pageUp: k.pageUp,
      pageDown: k.pageDown,
      home: k.home,
      end: k.end,
    });
    if (action.historyHandoff === "prev") {
      onHistoryPrev?.();
      return;
    }
    if (action.historyHandoff === "next") {
      onHistoryNext?.();
      return;
    }
    if (action.pasteRequest) {
      // Paste support is deferred to 5d-21f; for now insert the raw content.
      const v = valueRef.current;
      const c = cursorRef.current;
      const merged = v.slice(0, c) + action.pasteRequest.content + v.slice(c);
      apply(merged, c + action.pasteRequest.content.length);
      return;
    }
    if (action.submit) {
      onSubmit?.(action.submitValue ?? valueRef.current);
      return;
    }
    apply(action.next, action.cursor);
  });

  // Cursor positioning: split logical lines and project the (line, col) onto
  // the rendered rows. The prompt prefix sits on the FIRST logical line; the
  // following lines are gutter-padded by spaces of the same width.
  const lines = value.length === 0 ? [""] : value.split("\n");
  const { line: cursorLine, col: cursorCol } = lineAndColumn(value, cursor);
  const prefixCells = stringCells(prefix) + 1; // prefix + the gap=1 space
  const lineText = lines[cursorLine] ?? "";
  const cursorVisualCol = prefixCells + stringCells(lineText.slice(0, cursorCol));
  const rowFromBottom = lines.length - 1 - cursorLine;
  useCursor(disabled ? null : { col: cursorVisualCol, rowFromBottom, visible: true });

  const showPlaceholder = value.length === 0;
  const gutter = " ".repeat(stringCells(prefix));

  return (
    <inkCompat.Box flexDirection="column">
      {lines.map((ln, idx) => (
        <inkCompat.Box key={lineKey(ln, idx)} flexDirection="row" gap={1}>
          {idx === 0 ? (
            <inkCompat.Text color={TONE_BRAND} bold>
              {prefix}
            </inkCompat.Text>
          ) : (
            <inkCompat.Text color={FG_FAINT}>{gutter}</inkCompat.Text>
          )}
          {idx === 0 && showPlaceholder ? (
            <inkCompat.Text dimColor color={FG_FAINT}>
              {placeholder ?? "type a message…"}
            </inkCompat.Text>
          ) : (
            <inkCompat.Text color={FG_BODY}>{ln.length > 0 ? ln : " "}</inkCompat.Text>
          )}
        </inkCompat.Box>
      ))}
    </inkCompat.Box>
  );
}

function stringCells(s: string): number {
  if (s.length === 0) return 0;
  return stringWidth(s);
}

function lineKey(line: string, idx: number): string {
  return `${idx}-${line.length}`;
}
