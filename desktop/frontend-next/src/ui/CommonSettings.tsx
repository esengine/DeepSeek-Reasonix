import { useCallback } from "react";
import type { AgentPort, Appearance as Look } from "../port/port";
import { STORAGE as LANG_KEY, t } from "../i18n";
import { DefaultMode } from "./DefaultMode";
import { Notifications } from "./Notifications";
import { SendShortcut } from "./SendShortcut";
import { ThemeScheduleFields } from "./ThemeScheduleFields";
import { UpdateMode } from "./UpdateMode";
import { SectionCard } from "./SectionCard";
import { SegChoice, SettingRow } from "./SettingRow";
import { SCHEMES } from "./Appearance";

const LANGS: [string, string][] = [
  ["", "look.follow_the_system"],
  ["zh", "settings.text"],
  ["en", "settings.english"],
];

interface Props {
  port: AgentPort;
  homePort: AgentPort;
  theme: string;
  onTheme: (id: string) => void;
  look: Look;
  onLook: (look: Look) => void;
}

export function CommonSettings({ port, homePort, theme, onTheme, look, onLook }: Props) {
  const setLang = useCallback(
    (next: string) => {
      localStorage.setItem(LANG_KEY, next);
      onLook({ ...look, language: next });
      setTimeout(() => location.reload(), 120);
    },
    [look, onLook],
  );
  const langNow = localStorage.getItem(LANG_KEY) ?? look.language ?? "";
  return (
    <>
      <SectionCard title={t("common.start")}>
        <DefaultMode port={port} />
        <UpdateMode port={homePort} />
      </SectionCard>
      <SectionCard title={t("settings.input_and_reminders")}>
        <SettingRow id="language" title={t("common.language")} desc={t("settings.interface_language")}>
          <select aria-label={t("common.language")} data-action="appearance.language" value={langNow} onChange={(e) => setLang(e.target.value)}>
            {LANGS.map(([id, name]) => (
              <option key={id} value={id}>{t(name)}</option>
            ))}
          </select>
        </SettingRow>
        <SendShortcut />
        <Notifications port={port} />
        <SettingRow id="mode" title={t("settings.light_and_dark")} desc={t("settings.follow_the_system_light_or")}>
          <SegChoice label={t("settings.light_and_dark")} value={theme} action="appearance.scheme" onPick={onTheme} options={SCHEMES.map(([id, name]) => [id, t(name)] as const)} />
        </SettingRow>
        {theme === "scheduled" && <ThemeScheduleFields />}
      </SectionCard>
    </>
  );
}
