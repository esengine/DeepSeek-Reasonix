import { Box, Text, useStdout } from "ink";
import React from "react";
import { t } from "../../i18n/index.js";
import type { SlashCommandSpec, SlashGroup } from "./slash.js";
import { GLYPH, useColor } from "./theme.js";

const GROUP_MODE_MAX_ROWS = 24;
const SEARCH_MODE_MAX_ROWS = 8;
const COMMAND_NAME_CELLS = 14;
const ARGS_CELLS = 14;

export interface SlashSuggestionsProps {
  matches: SlashCommandSpec[] | null;
  selectedIndex: number;
  /** True when input is a bare `/` — render section headers + advanced footer. */
  groupMode?: boolean;
  /** Count of hidden `advanced` commands; rendered as a footer hint when groupMode is true. */
  advancedHidden?: number;
}

const GROUP_LABEL: Record<SlashGroup, string> = {
  chat: "CHAT",
  setup: "SETUP",
  info: "INFO",
  session: "SESSION",
  extend: "EXTEND",
  code: "CODE",
  jobs: "JOBS",
  advanced: "ADVANCED",
};

export function SlashSuggestions({
  matches,
  selectedIndex,
  groupMode,
  advancedHidden,
}: SlashSuggestionsProps): React.ReactElement | null {
  const color = useColor();
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const [rememberedWindowStart, setRememberedWindowStart] = React.useState(0);

  // All hooks must run on every render; the early-return branches below
  // would otherwise change hook count between renders → "Rendered more
  // hooks than during the previous render" crash when matches flips
  // between null/empty and non-empty.
  const maxRows = groupMode ? GROUP_MODE_MAX_ROWS : SEARCH_MODE_MAX_ROWS;
  const safeMatches = matches ?? [];
  const windowStart = computeWindowStart(
    safeMatches,
    maxRows,
    selectedIndex,
    rememberedWindowStart,
    groupMode,
  );
  React.useEffect(() => {
    setRememberedWindowStart(windowStart);
  }, [windowStart]);

  if (matches === null) return null;
  if (matches.length === 0) {
    return (
      <Box paddingX={1} marginTop={1}>
        <Text color={color.warn} bold>
          {GLYPH.warn}
        </Text>
        <Text> </Text>
        <Text color={color.warn}>no slash command matches that prefix</Text>
        <Text dimColor>{" — Backspace to edit, or /help for the full list"}</Text>
      </Box>
    );
  }
  const total = matches.length;
  const items = buildVisibleItems(matches, windowStart, maxRows, groupMode);
  const shownCommands = items.filter((item) => item.kind === "command");
  const hiddenAbove = windowStart;
  const hiddenBelow = total - windowStart - shownCommands.length;
  return (
    <Box flexDirection="column" paddingX={1} marginTop={1} flexShrink={0} flexWrap="nowrap">
      <Box>
        <Text color={color.accent} bold>
          {"/ "}
        </Text>
        <Text dimColor>{`${total} command${total === 1 ? "" : "s"}`}</Text>
        {hiddenAbove > 0 ? <Text dimColor>{`   ↑ ${hiddenAbove} above`}</Text> : null}
      </Box>
      {items.map((item) => {
        if (item.kind === "group") {
          return <GroupHeader key={`group:${item.group}:${item.beforeIndex}`} group={item.group} />;
        }
        return (
          <SuggestionRow
            key={`cmd:${item.spec.group}:${item.spec.cmd}`}
            spec={item.spec}
            isSelected={item.index === selectedIndex}
            columns={cols}
          />
        );
      })}
      {hiddenBelow > 0 ? <Text dimColor>{`   ↓ ${hiddenBelow} below`}</Text> : null}
      {groupMode && advancedHidden && advancedHidden > 0 ? (
        <Box marginTop={1}>
          <Text dimColor>{`  + ${advancedHidden} advanced  ·  type a letter to search`}</Text>
        </Box>
      ) : null}
      <Box marginTop={0}>
        <Text dimColor>{"  ↑↓ navigate · Tab / ⏎ pick · esc cancel"}</Text>
      </Box>
    </Box>
  );
}

export function computeWindowStart(
  matches: readonly SlashCommandSpec[],
  maxRows: number,
  selectedIndex: number,
  currentWindowStart: number,
  groupMode = false,
): number {
  if (matches.length <= 0) return 0;
  const maxWindowStart = Math.max(0, matches.length - 1);
  let start = Math.max(0, Math.min(currentWindowStart, maxWindowStart));
  const clampedSelectedIndex = Math.max(0, Math.min(selectedIndex, matches.length - 1));
  if (clampedSelectedIndex < start) start = clampedSelectedIndex;
  while (start < clampedSelectedIndex) {
    const visibleCommandIndexes = buildVisibleItems(matches, start, maxRows, groupMode)
      .filter((item) => item.kind === "command")
      .map((item) => item.index);
    if (visibleCommandIndexes.includes(clampedSelectedIndex)) break;
    start += 1;
  }
  return Math.min(start, maxWindowStart);
}

type VisibleSuggestionItem =
  | { kind: "group"; group: SlashGroup; beforeIndex: number }
  | { kind: "command"; spec: SlashCommandSpec; index: number };

export function buildVisibleItems(
  matches: readonly SlashCommandSpec[],
  windowStart: number,
  maxRows: number,
  groupMode = false,
): VisibleSuggestionItem[] {
  const out: VisibleSuggestionItem[] = [];
  for (let idx = windowStart; idx < matches.length && out.length < maxRows; idx += 1) {
    const spec = matches[idx]!;
    if (groupMode && shouldShowGroupHeader(matches, idx)) {
      if (out.length >= maxRows) break;
      out.push({ kind: "group", group: spec.group, beforeIndex: idx });
    }
    if (out.length >= maxRows) break;
    out.push({ kind: "command", spec, index: idx });
  }
  return out;
}

function shouldShowGroupHeader(matches: readonly SlashCommandSpec[], idx: number): boolean {
  return idx === 0 || matches[idx]?.group !== matches[idx - 1]?.group;
}

function GroupHeader({ group }: { group: SlashGroup }): React.ReactElement {
  return (
    <Box flexShrink={0} height={1} flexWrap="nowrap">
      <Text dimColor wrap="truncate">
        {`  ${GROUP_LABEL[group]}`}
      </Text>
    </Box>
  );
}

function SuggestionRow({
  spec,
  isSelected,
  columns,
}: {
  spec: SlashCommandSpec;
  isSelected: boolean;
  columns: number;
}) {
  const color = useColor();
  const name = `/${spec.cmd}`;
  const argsSuffix = spec.argsHint ? spec.argsHint : "";
  const key = `slash.${spec.cmd}.description`;
  const translated = t(key);
  const summary = translated === key ? spec.summary : translated;
  const aliasHint = spec.aliases?.length ? ` · /${spec.aliases.join(" /")}` : "";
  const reservedCells = 2 + COMMAND_NAME_CELLS + ARGS_CELLS + 2 + 2;
  const summaryBudget = Math.max(8, columns - reservedCells);
  const summaryText = truncateCells(`${summary}${aliasHint}`, summaryBudget);
  return (
    <Box flexDirection="row" flexWrap="nowrap" flexShrink={0} height={1} minHeight={1}>
      <Text color={isSelected ? color.primary : color.info} bold={isSelected} wrap="truncate">
        {isSelected ? `${GLYPH.cur} ` : "  "}
      </Text>
      <Text color={color.accent} bold={isSelected} wrap="truncate">
        {padOrTrim(name, COMMAND_NAME_CELLS)}
      </Text>
      <Text dimColor wrap="truncate">
        {padOrTrim(argsSuffix, ARGS_CELLS)}
      </Text>
      <Text wrap="truncate">{"  "}</Text>
      <Text color={isSelected ? color.user : color.info} dimColor={!isSelected} wrap="truncate">
        {summaryText}
      </Text>
    </Box>
  );
}

function padOrTrim(value: string, cells: number): string {
  const trimmed = truncateCells(value, cells);
  return trimmed.padEnd(cells);
}

function truncateCells(value: string, maxCells: number): string {
  if (value.length <= maxCells) return value;
  if (maxCells <= 1) return value.slice(0, Math.max(0, maxCells));
  return `${value.slice(0, maxCells - 1)}…`;
}
