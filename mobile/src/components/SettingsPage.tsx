import { t, type Locale } from "../i18n/messages";
import type { Platform } from "../lib/platform";

export type ThemePref = "system" | "light" | "dark";

export function SettingsPage({
  locale,
  theme,
  platform,
  showLargeTitle = true,
  onLocale,
  onTheme,
  onPlatform,
}: {
  locale: Locale;
  theme: ThemePref;
  platform: Platform;
  showLargeTitle?: boolean;
  onLocale: (l: Locale) => void;
  onTheme: (t: ThemePref) => void;
  onPlatform: (p: Platform) => void;
}) {
  return (
    <div className="page-scroll">
      {showLargeTitle ? (
        <h1 className="large-title">{t(locale, "settings.title")}</h1>
      ) : (
        <div style={{ height: 8 }} />
      )}

      <section className="list-section">
        <h2 className="list-section-header">{t(locale, "settings.sectionAppearance")}</h2>
        <div className="list-group">
          <div className="pref-row">
            <span className="pref-row-label">{t(locale, "settings.theme")}</span>
            <div className="segmented" role="group" aria-label={t(locale, "settings.theme")}>
              {(
                [
                  ["system", "settings.themeSystem"],
                  ["dark", "settings.themeDark"],
                  ["light", "settings.themeLight"],
                ] as const
              ).map(([value, key]) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={theme === value}
                  onClick={() => onTheme(value)}
                >
                  {t(locale, key)}
                </button>
              ))}
            </div>
          </div>
          <div className="pref-row">
            <span className="pref-row-label">{t(locale, "settings.platform")}</span>
            <div className="segmented" role="group" aria-label={t(locale, "settings.platform")}>
              {(
                [
                  ["ios", "iOS"],
                  ["android", "Android"],
                  ["web", "Web"],
                ] as const
              ).map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={platform === value}
                  onClick={() => onPlatform(value)}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
        </div>
      </section>

      <section className="list-section">
        <h2 className="list-section-header">{t(locale, "settings.sectionGeneral")}</h2>
        <div className="list-group">
          <div className="pref-row">
            <span className="pref-row-label">{t(locale, "settings.language")}</span>
            <div className="segmented" role="group" aria-label={t(locale, "settings.language")}>
              {(
                [
                  ["en", "EN"],
                  ["zh", "简"],
                  ["zh-TW", "繁"],
                ] as const
              ).map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={locale === value}
                  onClick={() => onLocale(value)}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
          <div className="pref-row">
            <span className="pref-row-label">{t(locale, "settings.account")}</span>
            <span className="pref-row-value">{t(locale, "common.offline")}</span>
          </div>
        </div>
        <p className="footnote">{t(locale, "settings.accountOptional")}</p>
      </section>
    </div>
  );
}
