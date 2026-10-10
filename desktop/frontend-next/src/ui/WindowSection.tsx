import { useCallback, useEffect, useState } from "react";
import type { AgentPort, TrayPrefs } from "../port/port";
import { t } from "../i18n";
import { keepsAwake, setKeepsAwake } from "../state/prefs";
import { setShowsReceipt, showsReceipt } from "../state/session";
import { ApplyNote } from "./Group";
import { Switch } from "./Switch";

export function WindowSection({ port }: { port: AgentPort }) {
  // null in a browser tab, where there is no window to keep running and no
  // icon to bring one back. The whole section goes with it.
  const [tray, setTray] = useState<TrayPrefs | null>(null);
  const [receipt, setReceipt] = useState(showsReceipt);
  const [awake, setAwake] = useState(keepsAwake);

  useEffect(() => {
    let live = true;
    port.trayPrefs().then((p) => live && setTray(p)).catch(() => live && setTray(null));
    return () => {
      live = false;
    };
  }, [port]);

  // The window answers with what is true afterwards rather than what was
  // asked: turning the icon off turns backgrounding off with it, and the
  // switch has to show that rather than the request.
  const flipTray = useCallback(
    (patch: Partial<TrayPrefs>) => {
      if (!tray) return;
      const next = { ...tray, ...patch };
      port
        .setTrayPrefs(next.icon, next.closeToTray)
        .then((got) => got && setTray(got))
        .catch(() => {});
    },
    [port, tray],
  );

  if (!tray) return null;
  return (
    <section className="grp" id="set-window" data-setting="window">
      <div className="grp-hd">
        <h3>{t("窗口")}</h3>
      </div>
      <p className="hint">
        {t("关闭窗口后需通过托盘图标重新打开主界面，下方选项依赖该图标。")}
      </p>
      <div className="grp-items">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("回合结束时给出回执")}</span>
            <span className="ds">{t("列出这一轮改了什么、验了什么、哪些没有验；无话可说时不出现。下一轮起生效")}</span>
          </span>
          <Switch
            data-action="chrome.receipt"
            on={receipt}
            label={t("回合结束时给出回执")}
            onClick={() => {
              setShowsReceipt(!receipt);
              setReceipt(!receipt);
            }}
          />
        </div>
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("任务运行时阻止系统休眠")}</span>
            <span className="ds">{t("有会话正在运行时，电脑不会自动进入睡眠（电池供电时也一样）；屏幕仍按系统设置熄灭，全部空闲后恢复。合上笔记本盖子不受此项控制")}</span>
          </span>
          <Switch
            data-action="chrome.keep-awake"
            on={awake}
            label={t("任务运行时阻止系统休眠")}
            onClick={() => {
              setKeepsAwake(!awake);
              setAwake(!awake);
            }}
          />
        </div>
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("在托盘显示图标")}</span>
            <span className="ds">
              {tray.icon === tray.live
                ? t("通过图标即可判断正在运行还是等待批准")
                : tray.icon
                  ? t("下次启动时出现")
                  : t("下次启动时不再显示，本次仍保留")}
            </span>
          </span>
          <Switch data-action="tray.icon" on={tray.icon} label={t("在托盘显示图标")} onClick={() => flipTray({ icon: !tray.icon })} />
        </div>
        {/* The second switch only means anything while there is an icon,
            so it is drawn as a branch of the first rather than as a rule
            you have to discover by watching it grey out. */}
        <div className="lrow subrow" data-off={tray.icon && tray.live ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("关闭窗口后在托盘中继续运行")}</span>
            <span className="ds">{t("关闭窗口不会中断会话和后台任务；从托盘菜单退出才会完全关闭程序")}</span>
          </span>
          <Switch
            data-action="tray.close-to-tray"
            on={tray.closeToTray}
            busy={!tray.icon || !tray.live}
            label={t("关闭窗口后在托盘中继续运行")}
            onClick={() => flipTray({ closeToTray: !tray.closeToTray })}
          />
        </div>
      </div>
      <ApplyNote id="window" />
    </section>
  );
}
