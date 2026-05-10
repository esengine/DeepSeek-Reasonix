import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React as a runtime value
import React, { useMemo, useState } from "react";
import { clipToCells } from "../../../frame/width.js";
import { t } from "../../../i18n/index.js";
import { writeClipboard } from "../clipboard.js";
import { useKeystroke } from "../keystroke-context.js";
import type { Card } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { type SnapshotLine, buildSnapshot, isYankable, yankRange } from "./snapshot.js";

export interface CopyModeProps {
  cards: ReadonlyArray<Card>;
  onClose: (yanked: { size: number; osc52: boolean; filePath: string | null } | null) => void;
}

const CHROME_ROWS = 3;

export function CopyMode({ cards, onClose }: CopyModeProps): React.ReactElement {
  const snapshot = useMemo(() => buildSnapshot(cards), [cards]);
  const { stdout } = useStdout();
  const termRows = stdout?.rows ?? 30;
  const termCols = stdout?.columns ?? 80;
  const bodyRows = Math.max(4, termRows - CHROME_ROWS);

  const lastYankableIdx = findLastYankable(snapshot);
  const initialCursor = findFirstYankable(snapshot);

  const [cursor, setCursor] = useState(initialCursor);
  const [anchor, setAnchor] = useState<number | null>(null);
  const [status, setStatus] = useState<string | null>(null);

  const stepDown = (i: number) => stepBy(snapshot, i, +1);
  const stepUp = (i: number) => stepBy(snapshot, i, -1);

  useKeystroke((ev) => {
    if (ev.escape || (ev.input === "q" && !ev.ctrl && !ev.meta)) return onClose(null);
    if (ev.input === "j" || ev.downArrow) return setCursor(stepDown(cursor));
    if (ev.input === "k" || ev.upArrow) return setCursor(stepUp(cursor));
    if (ev.pageDown) return setCursor(scrollBy(snapshot, cursor, bodyRows));
    if (ev.pageUp) return setCursor(scrollBy(snapshot, cursor, -bodyRows));
    if (ev.input === "g") return setCursor(initialCursor);
    if (ev.input === "G") return setCursor(lastYankableIdx);
    if (ev.input === "v" || ev.input === "V") {
      setAnchor((a) => (a === null ? cursor : null));
      return;
    }
    if (ev.input === "y" || ev.return) {
      const from = anchor ?? cursor;
      const to = cursor;
      const text = yankRange(snapshot, from, to).trim();
      if (text.length === 0) {
        setStatus(t("copyMode.statusEmpty"));
        return;
      }
      const w = writeClipboard(text);
      setStatus(t("copyMode.statusYanked", { size: text.length, osc52: w.osc52 ? "y" : "n" }));
      setTimeout(() => onClose(w), 600);
    }
  });

  const window = computeWindow(snapshot, cursor, bodyRows);
  const selRange =
    anchor === null ? null : ([Math.min(anchor, cursor), Math.max(anchor, cursor)] as const);
  const totalY = countYankable(snapshot);
  const cursorY = countYankableUntil(snapshot, cursor);

  return (
    <Box flexDirection="column">
      <Box>
        <Text color={TONE.brand} bold>
          {t("copyMode.title")}
        </Text>
        <Text color={FG.faint}>{`  ${t("copyMode.help")}`}</Text>
      </Box>
      <Box flexDirection="column">
        {snapshot.length === 0 ? (
          <Text color={FG.faint}>{t("copyMode.empty")}</Text>
        ) : (
          window.lines.map((line, i) => {
            const idx = window.start + i;
            return (
              <CopyLine
                key={`${line.cardId}-${idx}`}
                line={line}
                cols={termCols}
                isCursor={idx === cursor}
                inSelection={selRange !== null && idx >= selRange[0] && idx <= selRange[1]}
              />
            );
          })
        )}
      </Box>
      <Box>
        <Text color={FG.meta}>
          {t("copyMode.statusBar", {
            cur: cursorY > 0 ? cursorY : 1,
            total: Math.max(1, totalY),
            sel: anchor === null ? "—" : String(rangeYankable(snapshot, anchor, cursor)),
          })}
        </Text>
        {status ? <Text color={TONE.ok}>{`  ${status}`}</Text> : null}
      </Box>
    </Box>
  );
}

function CopyLine({
  line,
  cols,
  isCursor,
  inSelection,
}: {
  line: SnapshotLine;
  cols: number;
  isCursor: boolean;
  inSelection: boolean;
}): React.ReactElement {
  const marker = isCursor ? "▸ " : "  ";
  const room = Math.max(1, cols - 2);
  const display = line.kind === "blank" ? "" : clipToCells(line.text, room);
  if (line.kind === "header") {
    return (
      <Box>
        <Text color={isCursor ? TONE.brand : FG.faint}>{marker}</Text>
        <Text color={FG.meta}>{display}</Text>
      </Box>
    );
  }
  const color = isCursor ? TONE.brand : FG.body;
  return (
    <Box>
      <Text color={isCursor ? TONE.brand : FG.faint}>{marker}</Text>
      <Text color={color} inverse={inSelection}>
        {display.length === 0 ? " " : display}
      </Text>
    </Box>
  );
}

function findFirstYankable(snapshot: ReadonlyArray<SnapshotLine>): number {
  for (let i = 0; i < snapshot.length; i++) if (isYankable(snapshot[i])) return i;
  return 0;
}

function findLastYankable(snapshot: ReadonlyArray<SnapshotLine>): number {
  for (let i = snapshot.length - 1; i >= 0; i--) if (isYankable(snapshot[i])) return i;
  return Math.max(0, snapshot.length - 1);
}

function stepBy(snapshot: ReadonlyArray<SnapshotLine>, from: number, dir: 1 | -1): number {
  const last = snapshot.length - 1;
  let i = Math.max(0, Math.min(last, from + dir));
  while (i > 0 && i < last && snapshot[i]?.kind === "header") i += dir;
  if (i < 0) return 0;
  if (i > last) return last;
  return i;
}

function scrollBy(snapshot: ReadonlyArray<SnapshotLine>, from: number, delta: number): number {
  const last = snapshot.length - 1;
  return Math.max(0, Math.min(last, from + delta));
}

function computeWindow(
  snapshot: ReadonlyArray<SnapshotLine>,
  cursor: number,
  rows: number,
): { start: number; lines: SnapshotLine[] } {
  if (snapshot.length <= rows) return { start: 0, lines: snapshot.slice() };
  const half = Math.floor(rows / 2);
  let start = Math.max(0, cursor - half);
  if (start + rows > snapshot.length) start = snapshot.length - rows;
  return { start, lines: snapshot.slice(start, start + rows) };
}

function countYankable(snapshot: ReadonlyArray<SnapshotLine>): number {
  let n = 0;
  for (const line of snapshot) if (isYankable(line)) n++;
  return n;
}

function countYankableUntil(snapshot: ReadonlyArray<SnapshotLine>, idx: number): number {
  let n = 0;
  for (let i = 0; i <= Math.min(idx, snapshot.length - 1); i++) if (isYankable(snapshot[i])) n++;
  return n;
}

function rangeYankable(snapshot: ReadonlyArray<SnapshotLine>, a: number, b: number): number {
  const lo = Math.min(a, b);
  const hi = Math.max(a, b);
  let n = 0;
  for (let i = lo; i <= hi; i++) if (isYankable(snapshot[i])) n++;
  return n;
}
