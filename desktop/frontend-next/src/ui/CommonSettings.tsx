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
  ["", "跟随系统"],
  ["zh", "中文"],
  ["en", "英文"],
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
      <SectionCard title={t("开始")}>
        <DefaultMode port={port} />
        <UpdateMode port={homePort} />
      </SectionCard>
      <SectionCard title={t("输入与提醒")}>
        <SettingRow id="language" title={t("语言")} desc={t("界面语言")}>
          <select aria-label={t("语言")} data-action="appearance.language" value={langNow} onChange={(e) => setLang(e.target.value)}>
            {LANGS.map(([id, name]) => (
              <option key={id} value={id}>{t(name)}</option>
            ))}
          </select>
        </SettingRow>
        <SendShortcut />
        <Notifications port={port} />
        <SettingRow id="mode" title={t("明暗")} desc={t("跟随系统、浅色或深色")}>
          <SegChoice label={t("明暗")} value={theme} action="appearance.scheme" onPick={onTheme} options={SCHEMES.map(([id, name]) => [id, t(name)] as const)} />
        </SettingRow>
        {theme === "scheduled" && <ThemeScheduleFields />}
      </SectionCard>
    </>
  );
}
