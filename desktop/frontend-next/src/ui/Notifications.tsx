import { useCallback, useEffect, useState } from "react";
import type { AgentPort, NotifyPrefs } from "../port/port";
import { t } from "../i18n";
import { ApplyNote } from "./Group";
import { Switch } from "./Switch";

/** What this machine is allowed to interrupt for. A kernel with no window
 *  answers nothing and this draws nothing: a notification fired there would
 *  land on the server's desktop, not on the watcher's. */
export function Notifications({ port }: { port: AgentPort }) {
  const [prefs, setPrefs] = useState<NotifyPrefs | null>(null);

  useEffect(() => {
    let live = true;
    port.notifyPrefs().then((p) => live && setPrefs(p)).catch(() => live && setPrefs(null));
    return () => {
      live = false;
    };
  }, [port]);

  // The window answers with what is true afterwards, not with what was asked:
  // the same read is what a second window would get.
  const flip = useCallback(
    (patch: Partial<NotifyPrefs>) => {
      if (!prefs) return;
      port.setNotifyPrefs({ ...prefs, ...patch }).then((got) => got && setPrefs(got)).catch(() => {});
    },
    [port, prefs],
  );

  // A settings page must not grow a heading for a switch it cannot show.
  if (!prefs) return null;
  return (
    <section className="grp" id="set-notify" data-setting="notify">
      <div className="grp-hd">
        <h3>{t("通知")}</h3>
      </div>
      <p className="hint">{t("只在你没盯着窗口的时候有用：一轮跑完、停下来等批准、或者模型反过来问你，都可以让系统提醒一次。")}</p>
      <div className="grp-items">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("发送系统通知")}</span>
            <span className="ds">{t("窗口在后台时由系统弹出提醒。立即生效，正在跑的会话下一次事件就按新设置来")}</span>
          </span>
          <Switch data-action="notify.enabled" on={prefs.enabled} label={t("发送系统通知")} onClick={() => flip({ enabled: !prefs.enabled })} />
        </div>
        {/* Drawn as branches of the first rather than as a rule you discover by
            watching three switches grey out together. */}
        <div className="lrow subrow" data-off={prefs.enabled ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("这一轮结束时")}</span>
            <span className="ds">{t("回答写完、或者这一轮失败了")}</span>
          </span>
          <Switch data-action="notify.turn-done" on={prefs.turnDone} busy={!prefs.enabled} label={t("这一轮结束时")} onClick={() => flip({ turnDone: !prefs.turnDone })} />
        </div>
        <div className="lrow subrow" data-off={prefs.enabled ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("有操作等待批准时")}</span>
            <span className="ds">{t("模型要动的东西超出了已经给出的授权，正停在那里等你")}</span>
          </span>
          <Switch data-action="notify.approval" on={prefs.approval} busy={!prefs.enabled} label={t("有操作等待批准时")} onClick={() => flip({ approval: !prefs.approval })} />
        </div>
        <div className="lrow subrow" data-off={prefs.enabled ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("模型提问时")}</span>
            <span className="ds">{t("模型停下来问你一个问题，没有答案它就不会往下走")}</span>
          </span>
          <Switch data-action="notify.ask" on={prefs.ask} busy={!prefs.enabled} label={t("模型提问时")} onClick={() => flip({ ask: !prefs.ask })} />
        </div>
      </div>
      <ApplyNote id="notify" />
    </section>
  );
}
