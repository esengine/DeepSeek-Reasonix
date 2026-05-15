import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig.jsx = "react" needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../i18n/index.js";
import type { SlashCommandSpec } from "./slash.js";
import { GLYPH, useColor } from "./theme.js";
import type { AtPickerEntry } from "./useCompletionPickers.js";

export interface SlashArgPickerProps {
  /**
   * When set, render a picker with these matches (filter already
   * applied upstream). Null → not in picker mode; check `hintSpec`
   * for a usage hint instead.
   */
  matches: readonly string[] | null;
  /** Highlighted row within `matches`. */
  selectedIndex: number;
  /**
   * Spec of the command the user is typing args for. Used to render
   * the header label ("/edit <file>") even when matches is empty or
   * the caller wants a hint instead of a picker.
   */
  spec: SlashCommandSpec;
  /** What kind of arg guidance to render. */
  kind: "picker" | "hint";
  /** The user's partial input — shown in the "no matches" hint. */
  partial: string;
  /**
   * When the completer is `"path"`, carries the rich entries (with `isDir`)
   * so the picker can render a trailing `/` on directories.
   */
  pathCandidates?: readonly AtPickerEntry[] | null;
}

/**
 * Argument-level picker for a slash command. Mirrors the visual
 * layout of SlashSuggestions / AtMentionSuggestions so the UI stays
 * consistent across all three picker surfaces.
 */
export function SlashArgPicker({
  matches,
  selectedIndex,
  spec,
  kind,
  partial,
  pathCandidates,
}: SlashArgPickerProps): React.ReactElement | null {
  const color = useColor();
  const headerRow = (
    <Box>
      <Text color={color.accent} bold>
        {"/ "}
      </Text>
      <Text color={color.accent} bold>
        {`/${spec.cmd}`}
      </Text>
      {spec.argsHint ? <Text dimColor>{` ${spec.argsHint}`}</Text> : null}
      <Text dimColor>{`  ${spec.summary}`}</Text>
    </Box>
  );

  if (kind === "hint") {
    return (
      <Box paddingX={1} marginTop={1}>
        {headerRow}
      </Box>
    );
  }

  if (matches === null) return null;
  if (matches.length === 0) {
    return (
      <Box flexDirection="column" paddingX={1} marginTop={1}>
        {headerRow}
        <Box>
          <Text color={color.warn} bold>
            {GLYPH.warn}
          </Text>
          <Text color={color.warn}>{t("slashArgPicker.noMatch", { partial })}</Text>
          <Text dimColor>{t("slashArgPicker.keepTyping")}</Text>
        </Box>
      </Box>
    );
  }

  const MAX = 8;
  const total = matches.length;
  const windowStart =
    total <= MAX ? 0 : Math.max(0, Math.min(selectedIndex - Math.floor(MAX / 2), total - MAX));
  const shown = matches.slice(windowStart, windowStart + MAX);
  const hiddenAbove = windowStart;
  const hiddenBelow = total - windowStart - shown.length;
  return (
    <Box flexDirection="column" paddingX={1} marginTop={1}>
      {headerRow}
      {hiddenAbove > 0 ? (
        <Text dimColor>{t("slashArgPicker.above", { hidden: hiddenAbove })}</Text>
      ) : null}
      {shown.map((value, i) => {
        const idx = windowStart + i;
        const isDir = pathCandidates?.[idx]?.isDir ?? false;
        return (
          <ArgRow key={value} value={value} isSelected={idx === selectedIndex} isDir={isDir} />
        );
      })}
      {hiddenBelow > 0 ? (
        <Text dimColor>{t("slashArgPicker.below", { hidden: hiddenBelow })}</Text>
      ) : null}
      <Box marginTop={0}>
        <Text dimColor>{t("slashArgPicker.footer")}</Text>
      </Box>
    </Box>
  );
}

function ArgRow({
  value,
  isSelected,
  isDir,
}: {
  value: string;
  isSelected: boolean;
  isDir: boolean;
}) {
  const color = useColor();
  return (
    <Box>
      <Text color={isSelected ? color.primary : color.info} bold={isSelected}>
        {isSelected ? `${GLYPH.cur} ` : "  "}
      </Text>
      <Text color={isSelected ? color.user : color.info} bold={isSelected} dimColor={!isSelected}>
        {value}
      </Text>
      {isDir ? <Text dimColor>/</Text> : null}
    </Box>
  );
}
