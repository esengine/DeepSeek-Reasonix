import { t } from "../i18n";
import { SCHEMES } from "./Appearance";
import { SegChoice, SettingRow } from "./SettingRow";
import { ThemeScheduleFields } from "./ThemeScheduleFields";

export function ThemeModeRow({ theme, onTheme }: { theme: string; onTheme: (id: string) => void }) {
  return (
    <>
      <SettingRow id="mode" title={t("settings.light_and_dark")} desc={t("settings.follow_the_system_light_or")}>
        <SegChoice label={t("settings.light_and_dark")} value={theme} action="appearance.scheme" onPick={onTheme} options={SCHEMES.map(([id, name]) => [id, t(name)] as const)} />
      </SettingRow>
      {theme === "scheduled" && <ThemeScheduleFields />}
    </>
  );
}
