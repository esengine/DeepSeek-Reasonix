/** Multi-line prompt input for chat-v2 — useCursor + useKeystroke, no Ink. */

// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import stringWidthLib from "string-width";
import { inkCompat, useCursor, useKeystroke } from "../../renderer/index.js";
import { lineAndColumn, processMultilineKey } from "./multiline-keys.js";
import {
  PASTE_SENTINEL_RANGE,
  type PasteEntry,
  decodePasteSentinel,
  encodePasteSentinel,
  expandPasteSentinels,
  formatBytesShort,
  makePasteEntry,
} from "./paste-sentinels.js";

const FG_BODY = "#c9d1d9";
const FG_FAINT = "#6e7681";
const TONE_BRAND = "#79c0ff";
const TONE_PASTE = "#d2a8ff";

export interface SimplePromptInputProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  /** Called with the EXPANDED value — paste sentinels are replaced with their original content. */
  readonly onSubmit?: (value: string) => void;
  readonly onCancel?: () => void;
  readonly onHistoryPrev?: () => void;
  readonly onHistoryNext?: () => void;
  readonly placeholder?: string;
  readonly prefix?: string;
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

  const valueRef = React.useRef(value);
  valueRef.current = value;
  const cursorRef = React.useRef(cursor);
  cursorRef.current = cursor;

  // Paste registry — keyed by sentinel id, holds original content. The id is
  // a small integer that we encode as a single PUA codepoint in the buffer
  // (see paste-sentinels.ts) so cursor arithmetic stays in char units.
  const pastesRef = React.useRef<Map<number, PasteEntry>>(new Map());
  const nextPasteIdRef = React.useRef(0);

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

  const registerPaste = (content: string): void => {
    const v = valueRef.current;
    const c = cursorRef.current;
    const id = nextPasteIdRef.current % PASTE_SENTINEL_RANGE;
    nextPasteIdRef.current = id + 1;
    pastesRef.current.set(id, makePasteEntry(id, content));
    const sentinel = encodePasteSentinel(id);
    apply(v.slice(0, c) + sentinel + v.slice(c), c + 1);
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
      registerPaste(action.pasteRequest.content);
      return;
    }
    if (action.submit) {
      const raw = action.submitValue ?? valueRef.current;
      onSubmit?.(expandPasteSentinels(raw, pastesRef.current));
      return;
    }
    apply(action.next, action.cursor);
  });

  // Cursor positioning: project (line, col) onto rendered rows. Sentinels
  // expand to their placeholder width; everything else uses stringWidth.
  const lines = value.length === 0 ? [""] : value.split("\n");
  const { line: cursorLine, col: cursorCol } = lineAndColumn(value, cursor);
  const prefixCells = stringCells(prefix) + 1; // prefix + gap=1 space
  const lineText = lines[cursorLine] ?? "";
  const cursorVisualCol =
    prefixCells + measureCells(lineText.slice(0, cursorCol), pastesRef.current);
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
            <LineSegments line={ln} pastes={pastesRef.current} />
          )}
        </inkCompat.Box>
      ))}
    </inkCompat.Box>
  );
}

interface LineSegmentsProps {
  readonly line: string;
  readonly pastes: ReadonlyMap<number, PasteEntry>;
}

function LineSegments({ line, pastes }: LineSegmentsProps): React.ReactElement {
  if (line.length === 0) {
    return <inkCompat.Text color={FG_BODY}> </inkCompat.Text>;
  }
  // Walk the line and split into runs of text vs paste sentinels so each
  // segment renders with its own style.
  const segments: React.ReactNode[] = [];
  let buf = "";
  let segIdx = 0;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]!;
    const id = decodePasteSentinel(ch);
    if (id === null) {
      buf += ch;
      continue;
    }
    if (buf.length > 0) {
      segments.push(
        <inkCompat.Text key={`seg-${segIdx++}-t`} color={FG_BODY}>
          {buf}
        </inkCompat.Text>,
      );
      buf = "";
    }
    const entry = pastes.get(id);
    segments.push(
      <inkCompat.Text key={`seg-${segIdx++}-p${id}`} color={TONE_PASTE}>
        {pasteLabel(id, entry)}
      </inkCompat.Text>,
    );
  }
  if (buf.length > 0) {
    segments.push(
      <inkCompat.Text key={`seg-${segIdx++}-t`} color={FG_BODY}>
        {buf}
      </inkCompat.Text>,
    );
  }
  return <>{segments}</>;
}

function pasteLabel(id: number, entry: PasteEntry | undefined): string {
  if (!entry) return `[paste #${id + 1} · (missing)]`;
  return `[paste #${id + 1} · ${entry.lineCount}l · ${formatBytesShort(entry.charCount)}]`;
}

function stringCells(s: string): number {
  if (s.length === 0) return 0;
  return stringWidthLib(s);
}

/** Visual width with paste sentinels expanded to their placeholder. */
function measureCells(s: string, pastes: ReadonlyMap<number, PasteEntry>): number {
  let n = 0;
  let plain = "";
  for (let i = 0; i < s.length; i++) {
    const ch = s[i]!;
    const id = decodePasteSentinel(ch);
    if (id === null) {
      plain += ch;
      continue;
    }
    if (plain.length > 0) {
      n += stringWidthLib(plain);
      plain = "";
    }
    n += pasteLabel(id, pastes.get(id)).length;
  }
  if (plain.length > 0) n += stringWidthLib(plain);
  return n;
}

function lineKey(line: string, idx: number): string {
  return `${idx}-${line.length}`;
}
