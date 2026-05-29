import { t } from "../../../i18n/index.js";
import type { ThemeName } from "./tokens.js";

export type ThemeChoice = ThemeName | "auto";

export function themeChoiceLabel(value: ThemeChoice): string {
  if (value === "auto") return `${t("themePicker.autoLabel")} (auto)`;
  const key = `wizard.themeName.${value}`;
  const label = t(key);
  return label === key ? value : `${label} (${value})`;
}
