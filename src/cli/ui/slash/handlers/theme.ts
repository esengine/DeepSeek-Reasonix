import { resolveThemePreference, saveTheme } from "@/config.js";
import { type ThemeName, isThemeName, listThemeNames } from "../../theme/tokens.js";
import type { SlashHandler } from "../dispatch.js";

const themeChoices = ["auto", ...listThemeNames()] as const;

function isThemeChoice(value: string): value is ThemeName | "auto" {
  return value === "auto" || isThemeName(value);
}

const theme: SlashHandler = (args) => {
  const next = args[0];
  if (!next) return { openThemePicker: true };

  if (!isThemeChoice(next)) {
    return { info: `unknown theme: ${next}\navailable: ${themeChoices.join(", ")}` };
  }

  saveTheme(next);
  const active = resolveThemePreference(next, process.env.REASONIX_THEME);
  return { info: `theme saved: ${next}\nactive on next launch: ${active}` };
};

export const handlers: Record<string, SlashHandler> = {
  theme,
};
