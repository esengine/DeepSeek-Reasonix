import { useCallback } from "react";
import type { AgentPort, Appearance as Look } from "../port/port";
import { STORAGE as LANG_KEY, t } from "../i18n";
import { ApplyNote, Group } from "./Group";
import { DefaultMode } from "./PlanApproval";
import { Notifications } from "./Notifications";
import { SendShortcut } from "./SendShortcut";
import { ThemeScheduleFields } from "./ThemeScheduleFields";
import { UpdateMode } from "./UpdateMode";
import { SectionCard } from "./SectionCard";
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
      <Group id="update-mode"
        title={t("更新方式")}
        hint={t("决定窗口自己做不做事：两种方式都不会替你安装新版本，重启始终由你决定。")}
      >
        <UpdateMode port={homePort} />
      </Group>
      </SectionCard>
      <SectionCard title={t("输入与提醒")}>
        <section className="grp" id="set-language" data-setting="language">
          <div className="grp-hd">
            <h3>{t("语言")}</h3>
          </div>
          <p className="hint">{t("界面显示语言。模型回复所用的语言与此设置无关，将跟随你发送消息时使用的语言。")}</p>
          <div className="grp-items">
            <select aria-label={t("语言")} data-action="appearance.language" value={langNow} onChange={(e) => setLang(e.target.value)}>
              {LANGS.map(([id, name]) => (
                <option key={id} value={id}>{t(name)}</option>
              ))}
            </select>
          </div>
          <ApplyNote id="language" />
        </section>
        <SendShortcut />
        <Notifications port={port} />
      <section className="grp" id="set-mode" data-setting="mode">
        <div className="grp-hd">
          <h3>{t("明暗")}</h3>
        </div>
        <p className="hint">{t("跟随系统时会随系统主题切换；浅色和深色保持固定；定时按本机时间每天切换。")}</p>
        <div className="grp-items">
          <div className="seg" data-text role="group" aria-label={t("明暗")}>
            {SCHEMES.map(([id, name]) => (
              <button key={id} data-action="appearance.scheme" data-value={id} aria-pressed={theme === id} onClick={() => onTheme(id)}>
                {t(name)}
              </button>
            ))}
          </div>
          {theme === "scheduled" && <ThemeScheduleFields />}
        </div>
        <ApplyNote id="mode" />
      </section>
      </SectionCard>
    </>
  );
}
