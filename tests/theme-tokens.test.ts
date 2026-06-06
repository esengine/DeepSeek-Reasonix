import { describe, expect, it } from "vitest";
import { THEME_STYLES as DASHBOARD_THEME_STYLES } from "../dashboard/src/theme.js";
import { THEME_STYLES as DESKTOP_THEME_STYLES } from "../desktop/src/theme.js";
import { COLOR, GRADIENT } from "../src/cli/ui/theme.js";
import {
  DEFAULT_THEME_NAME,
  FG,
  THEMES,
  listThemeNames,
  resolveThemeName,
  setActiveTheme,
  themeTokens,
} from "../src/cli/ui/theme/tokens.js";
import { EN } from "../src/i18n/EN.js";
import { JA } from "../src/i18n/JA.js";
import { de } from "../src/i18n/de.js";
import { ru } from "../src/i18n/ru.js";
import { zhCN } from "../src/i18n/zh-CN.js";

const CLI_LOCALES = [EN, zhCN, JA, de, ru];

describe("theme tokens", () => {
  it("resolves missing, auto, and invalid names to the default theme", () => {
    expect(resolveThemeName()).toBe(DEFAULT_THEME_NAME);
    expect(resolveThemeName("auto")).toBe(DEFAULT_THEME_NAME);
    expect(resolveThemeName("unknown")).toBe(DEFAULT_THEME_NAME);
  });

  it("lists all registered themes", () => {
    expect(listThemeNames()).toEqual([
      "graphite",
      "ember",
      "aurora",
      "sandstone",
      "porcelain",
      "linen",
      "glacier",
      "midnight",
    ]);
  });

  it("provides complete token sets for every theme", () => {
    for (const name of listThemeNames()) {
      const theme = THEMES[name];
      expect(theme.fg.body).toBeTruthy();
      expect(theme.tone.brand).toBeTruthy();
      expect(theme.toneActive.brand).toBeTruthy();
      expect(theme.surface.bg).toBeTruthy();
      expect(theme.card.error.color).toBe(theme.tone.err);
      expect(theme.card.streaming.color).toBe(theme.tone.brand);
    }
  });

  it("provides wizard labels and captions for every registered theme", () => {
    for (const locale of CLI_LOCALES) {
      for (const name of listThemeNames()) {
        expect(locale.wizard.themeName[name]).toBeTruthy();
        expect(locale.wizard.themeCaption[name]).toBeTruthy();
      }
    }
  });

  it("keeps public theme names aligned across CLI, dashboard, and desktop", () => {
    expect(listThemeNames()).toEqual([...DASHBOARD_THEME_STYLES]);
    expect(listThemeNames()).toEqual([...DESKTOP_THEME_STYLES]);
  });

  it("returns theme tokens by resolved name", () => {
    expect(themeTokens("light")).toBe(THEMES.light);
    expect(themeTokens("bad-name")).toBe(THEMES.dark);
  });

  it("keeps legacy token exports bound to the active theme", () => {
    const restore = setActiveTheme(THEMES.light);
    expect(FG.body).toBe(THEMES.light.fg.body);
    expect(COLOR.primary).toBe(THEMES.light.tone.brand);

    restore();
  });

  it("keeps legacy token exports object-compatible", () => {
    const restore = setActiveTheme(THEMES.light);

    expect(Object.keys(FG)).toEqual(Object.keys(THEMES.light.fg));
    expect({ ...COLOR }).toMatchObject({ primary: THEMES.light.tone.brand });
    expect(JSON.parse(JSON.stringify(COLOR))).toMatchObject({
      primary: THEMES.light.tone.brand,
    });
    expect(Array.isArray(GRADIENT)).toBe(true);
    expect([...GRADIENT]).toEqual([
      THEMES.light.tone.ok,
      THEMES.light.tone.brand,
      THEMES.light.tone.info,
      THEMES.light.toneActive.brand,
      THEMES.light.toneActive.violet,
      THEMES.light.tone.accent,
      THEMES.light.toneActive.accent,
      THEMES.light.tone.err,
    ]);

    restore();
  });

  it("restores legacy token exports after scoped active theme cleanup", () => {
    const restoreDark = setActiveTheme(THEMES.dark);
    const restoreLight = setActiveTheme(THEMES.light);

    expect(FG.body).toBe(THEMES.light.fg.body);
    restoreLight();
    expect(FG.body).toBe(THEMES.dark.fg.body);
    restoreDark();
  });
});
