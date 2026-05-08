import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig.jsx = "react" needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../i18n/index.js";
import type { SlashCommandSpec, SlashGroup } from "./slash.js";
import { COLOR, GLYPH } from "./theme.js";

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
  if (matches === null) return null;
  if (matches.length === 0) {
    return (
      <Box paddingX={1} marginTop={1}>
        <Text color={COLOR.warn} bold>
          {GLYPH.warn}
        </Text>
        <Text> </Text>
        <Text color={COLOR.warn}>no slash command matches that prefix</Text>
        <Text dimColor>{" — Backspace to edit, or /help for the full list"}</Text>
      </Box>
    );
  }
  const MAX = groupMode ? 24 : 8;
  const total = matches.length;
  const windowStart =
    total <= MAX ? 0 : Math.max(0, Math.min(selectedIndex - Math.floor(MAX / 2), total - MAX));
  const shown = matches.slice(windowStart, windowStart + MAX);
  const hiddenAbove = windowStart;
  const hiddenBelow = total - windowStart - shown.length;
  let lastGroup: SlashGroup | null = null;
  if (windowStart > 0) lastGroup = matches[windowStart - 1]?.group ?? null;
  return (
    <Box flexDirection="column" paddingX={1} marginTop={1}>
      <Box>
        <Text color={COLOR.accent} bold>
          {"/ "}
        </Text>
        <Text dimColor>{`${total} command${total === 1 ? "" : "s"}`}</Text>
        {hiddenAbove > 0 ? <Text dimColor>{`   ↑ ${hiddenAbove} above`}</Text> : null}
      </Box>
      {shown.map((spec, i) => {
        const idx = windowStart + i;
        const showHeader = groupMode && spec.group !== lastGroup;
        lastGroup = spec.group;
        return (
          <React.Fragment key={spec.cmd}>
            {showHeader ? (
              <Box marginTop={idx === 0 ? 0 : 1}>
                <Text dimColor>{`  ${GROUP_LABEL[spec.group]}`}</Text>
              </Box>
            ) : null}
            <SuggestionRow spec={spec} isSelected={idx === selectedIndex} />
          </React.Fragment>
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

function SuggestionRow({ spec, isSelected }: { spec: SlashCommandSpec; isSelected: boolean }) {
  const name = `/${spec.cmd}`;
  const argsSuffix = spec.argsHint ? spec.argsHint : "";
  const key = `slash.${spec.cmd}.description`;
  const translated = t(key);
  const summary = translated === key ? spec.summary : translated;
  const aliasHint = spec.aliases?.length ? ` · /${spec.aliases.join(" /")}` : "";
  return (
    <Box>
      <Text color={isSelected ? COLOR.primary : COLOR.info} bold={isSelected}>
        {isSelected ? `${GLYPH.cur} ` : "  "}
      </Text>
      <Text color={COLOR.accent} bold={isSelected}>
        {name.padEnd(14)}
      </Text>
      <Text dimColor>{argsSuffix.padEnd(14)}</Text>
      <Text>{"  "}</Text>
      <Text color={isSelected ? COLOR.user : COLOR.info} dimColor={!isSelected}>
        {summary}
      </Text>
      {aliasHint ? <Text dimColor>{aliasHint}</Text> : null}
    </Box>
  );
}
