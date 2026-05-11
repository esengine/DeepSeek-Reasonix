import { Box, Text } from "ink";
import React from "react";
import { t } from "../../i18n/index.js";
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
      <Text bold>{t("themePicker.header")}</Text>
      <SingleSelect
        items={items}
        initialValue={currentPreference}
        onSubmit={(value) => onChoose({ kind: "select", value })}
        onCancel={() => onChoose({ kind: "quit" })}
        footer={t("themePicker.footer")}
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
  if (value === currentPreference) tags.push(t("themePicker.currentPref"));
  if (value === activeTheme) tags.push(t("themePicker.activeNow"));
  if (value === "auto") tags.push(t("themePicker.autoDesc"));
  return tags.join(" · ");
}
