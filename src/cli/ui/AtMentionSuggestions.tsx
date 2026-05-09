import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig.jsx = "react" needs React in value scope for JSX compilation
import React from "react";
import { GLYPH, useColor } from "./theme.js";
import type { AtPickerEntry, AtPickerState } from "./useCompletionPickers.js";

export interface AtMentionSuggestionsProps {
  state: AtPickerState | null;
  selectedIndex: number;
}

const ROW_WINDOW = 8;

export function AtMentionSuggestions({
  state,
  selectedIndex,
}: AtMentionSuggestionsProps): React.ReactElement | null {
  const color = useColor();
  if (!state) return null;

  const isBrowse = state.kind === "browse";
  const entries = state.entries;
  const total = entries.length;
  const windowStart =
    total <= ROW_WINDOW
      ? 0
      : Math.max(0, Math.min(selectedIndex - Math.floor(ROW_WINDOW / 2), total - ROW_WINDOW));
  const shown = entries.slice(windowStart, windowStart + ROW_WINDOW);
  const hiddenAbove = windowStart;
  const hiddenBelow = total - windowStart - shown.length;

  return (
    <Box flexDirection="column" paddingX={1} marginTop={1}>
      <HeaderRow state={state} hiddenAbove={hiddenAbove} />
      {total === 0 ? <EmptyRow state={state} color={color} /> : null}
      {shown.map((entry, i) => (
        <EntryRow
          key={`${entry.insertPath}:${entry.isDir ? "d" : "f"}`}
          entry={entry}
          isSelected={windowStart + i === selectedIndex}
        />
      ))}
      {hiddenBelow > 0 ? <Text dimColor>{`   ↓ ${hiddenBelow} below`}</Text> : null}
      <FooterRow isBrowse={isBrowse} hasFolder={shown.some((e) => e.isDir)} />
    </Box>
  );
}

function HeaderRow({
  state,
  hiddenAbove,
}: {
  state: AtPickerState;
  hiddenAbove: number;
}) {
  const color = useColor();
  const total = state.entries.length;
  const lead = (
    <Text color={color.primary} bold>
      {"@ "}
    </Text>
  );
  if (state.kind === "browse") {
    const where = state.baseDir === "" ? "/" : `${state.baseDir}/`;
    const counter = state.loading ? "loading…" : `${total} ${total === 1 ? "entry" : "entries"}`;
    return (
      <Box>
        {lead}
        <Text dimColor>{`${where}  ${counter}`}</Text>
        {hiddenAbove > 0 ? <Text dimColor>{`   ↑ ${hiddenAbove} above`}</Text> : null}
      </Box>
    );
  }
  const status = state.searching
    ? `searching… ${state.scanned} scanned · ${total} ${total === 1 ? "match" : "matches"}`
    : `${total} ${total === 1 ? "match" : "matches"} for "${state.filter}"`;
  return (
    <Box>
      {lead}
      <Text dimColor>{status}</Text>
      {hiddenAbove > 0 ? <Text dimColor>{`   ↑ ${hiddenAbove} above`}</Text> : null}
    </Box>
  );
}

function EmptyRow({ state, color }: { state: AtPickerState; color: ReturnType<typeof useColor> }) {
  if (state.kind === "browse") {
    if (state.loading) return null;
    return (
      <Box>
        <Text color={color.warn} bold>
          {GLYPH.warn}
        </Text>
        <Text> </Text>
        <Text color={color.warn}>empty directory</Text>
      </Box>
    );
  }
  if (state.searching) {
    return (
      <Box>
        <Text dimColor>scanning the tree…</Text>
      </Box>
    );
  }
  return (
    <Box>
      <Text color={color.warn} bold>
        {GLYPH.warn}
      </Text>
      <Text> </Text>
      <Text color={color.warn}>{`no files match "${state.filter}"`}</Text>
    </Box>
  );
}

function EntryRow({ entry, isSelected }: { entry: AtPickerEntry; isSelected: boolean }) {
  const color = useColor();
  const cursor = isSelected ? `${GLYPH.cur} ` : "  ";
  const labelColor = entry.isDir ? color.accent : color.primary;
  const labelText = entry.isDir ? `${entry.label}/` : entry.label;
  return (
    <Box>
      <Text color={isSelected ? color.primary : color.info} bold={isSelected}>
        {cursor}
      </Text>
      <Text color={labelColor} bold={isSelected}>
        {labelText.padEnd(20)}
      </Text>
      {entry.dirSuffix ? <Text dimColor>{`  ${entry.dirSuffix}`}</Text> : null}
    </Box>
  );
}

function FooterRow({ isBrowse, hasFolder }: { isBrowse: boolean; hasFolder: boolean }) {
  const hint = isBrowse
    ? hasFolder
      ? "↑↓ navigate · Tab drill into folder · ⏎ insert · esc cancel"
      : "↑↓ navigate · Tab / ⏎ insert as @path · esc cancel"
    : "↑↓ navigate · Tab / ⏎ insert as @path · esc cancel";
  return (
    <Box marginTop={0}>
      <Text dimColor>{`  ${hint}`}</Text>
    </Box>
  );
}
