import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useState } from "react";
import stringWidth from "string-width";
import { t } from "../../i18n/index.js";
import { useKeystroke } from "./keystroke-context.js";
import { FG, SURFACE, TONE } from "./theme/tokens.js";

export interface UserTurnEntry {
  /** Card id in AgentState.cards — used to splice on selection. */
  cardId: string;
  /** 0-based count of user turns; maps to entries[] role==="user" by ordinal. */
  userTurnIndex: number;
  /** Verbatim user text — pre-fills the composer on selection. */
  text: string;
  ts: number;
}

export type EditPickerOutcome = { kind: "pick"; entry: UserTurnEntry } | { kind: "cancel" };

const PAGE_MARGIN = 6;
const PREVIEW_CELLS = 70;

export function EditPicker({
  entries,
  onChoose,
}: {
  entries: ReadonlyArray<UserTurnEntry>;
  onChoose: (outcome: EditPickerOutcome) => void;
}): React.ReactElement {
  const [focus, setFocus] = useState(Math.max(0, entries.length - 1));
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;
  const visibleCount = Math.max(3, rows - PAGE_MARGIN);
  const maxFocus = Math.max(0, entries.length - 1);

  useEffect(() => {
    setFocus((f) => Math.max(0, Math.min(f, maxFocus)));
  }, [maxFocus]);

  useKeystroke((ev) => {
    if (ev.escape) return onChoose({ kind: "cancel" });
    if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
    if (ev.downArrow) return setFocus((f) => Math.min(maxFocus, f + 1));
    if (ev.pageUp) return setFocus((f) => Math.max(0, f - visibleCount));
    if (ev.pageDown) return setFocus((f) => Math.min(maxFocus, f + visibleCount));
    if (ev.home) return setFocus(0);
    if (ev.end) return setFocus(maxFocus);
    if (ev.return) {
      const entry = entries[focus];
      if (entry) onChoose({ kind: "pick", entry });
    }
  });

  if (entries.length === 0) {
    return (
      <Box flexDirection="column" paddingY={1} paddingX={2}>
        <Text color={TONE.warn}>{t("editPicker.empty")}</Text>
        <Text color={FG.faint}>{t("editPicker.dismiss")}</Text>
      </Box>
    );
  }

  const start = Math.max(
    0,
    Math.min(focus - Math.floor(visibleCount / 2), entries.length - visibleCount),
  );
  const shown = entries.slice(start, start + visibleCount);

  return (
    <Box flexDirection="column" paddingY={1} paddingX={2}>
      <Text bold color={TONE.brand}>
        {t("editPicker.title")}
      </Text>
      <Text color={FG.faint}>{t("editPicker.hint")}</Text>
      <Box flexDirection="column" marginTop={1}>
        {shown.map((entry, i) => {
          const globalIdx = start + i;
          const focused = globalIdx === focus;
          return <Row key={entry.cardId} entry={entry} focused={focused} />;
        })}
      </Box>
    </Box>
  );
}

function Row({ entry, focused }: { entry: UserTurnEntry; focused: boolean }): React.ReactElement {
  const marker = focused ? "▸" : " ";
  const preview = oneLinePreview(entry.text, PREVIEW_CELLS);
  const numLabel = `#${entry.userTurnIndex + 1}`;
  const bg = focused ? SURFACE.bgElev : undefined;
  const fg = focused ? FG.strong : FG.body;
  return (
    <Box flexDirection="row" gap={1}>
      <Text color={focused ? TONE.brand : FG.faint} backgroundColor={bg}>
        {marker}
      </Text>
      <Text color={FG.meta} backgroundColor={bg}>
        {numLabel}
      </Text>
      <Text color={fg} backgroundColor={bg}>
        {preview}
      </Text>
    </Box>
  );
}

function oneLinePreview(text: string, cells: number): string {
  const firstLine = text.split(/\n/, 1)[0] ?? "";
  if (stringWidth(firstLine) <= cells) return firstLine;
  let s = "";
  let w = 0;
  for (const ch of firstLine) {
    const cw = stringWidth(ch);
    if (w + cw > cells - 1) break;
    s += ch;
    w += cw;
  }
  return `${s}…`;
}
