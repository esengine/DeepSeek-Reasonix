import { Box, Text } from "ink";
import React from "react";
import { type SelectItem, SingleSelect } from "./Select.js";
import { type ThemeName, listThemeNames } from "./theme/tokens.js";

export type ThemeChoice = ThemeName | "auto";

export type ThemePickerOutcome = { kind: "select"; value: ThemeChoice } | { kind: "quit" };

export function ThemePicker({
  currentPreference,
  activeTheme,
  onChoose,
}: {
  currentPreference: ThemeChoice;
  activeTheme: ThemeName;
  onChoose: (outcome: ThemePickerOutcome) => void;
}) {
  const choices: ThemeChoice[] = ["auto", ...listThemeNames()];
  const items: SelectItem<ThemeChoice>[] = choices.map((value) => ({
    value,
    label: value,
    hint: describeTheme(value, currentPreference, activeTheme),
  }));

  return (
    <Box flexDirection="column" marginY={1}>
      <Text bold>Theme</Text>
      <SingleSelect
        items={items}
        initialValue={currentPreference}
        onSubmit={(value) => onChoose({ kind: "select", value })}
        onCancel={() => onChoose({ kind: "quit" })}
        footer="↑↓ pick · ⏎ confirm · esc cancel"
      />
    </Box>
  );
}

function describeTheme(
  value: ThemeChoice,
  currentPreference: ThemeChoice,
  activeTheme: ThemeName,
): string {
  const tags: string[] = [];
  if (value === currentPreference) tags.push("current preference");
  if (value === activeTheme) tags.push("active now");
  if (value === "auto") tags.push("use REASONIX_THEME or default");
  return tags.join(" · ");
}
