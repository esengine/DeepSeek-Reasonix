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
  // The occasions below are for either switch: a person may want the chime and no toast.
  const active = prefs.enabled || prefs.sound;
  return (
    <section className="grp" id="set-notify" data-setting="notify">
      <div className="grp-hd">
        <h3>{t("common.notifications")}</h3>
      </div>
      <p className="hint">{t("notifications.sendSystemNotificationsWhenA")}</p>
      <div className="grp-items">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("notifications.sendSystemNotifications")}</span>
            <span className="ds">{t("notifications.offByDefaultWhenOn")}</span>
          </span>
          <Switch data-action="notify.enabled" on={prefs.enabled} label={t("notifications.sendSystemNotifications")} onClick={() => flip({ enabled: !prefs.enabled })} />
        </div>
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("notifications.playASound")}</span>
            <span className="ds">{t("notifications.offByDefaultPlaysA")}</span>
          </span>
          <Switch data-action="notify.sound" on={prefs.sound} label={t("notifications.playASound")} onClick={() => flip({ sound: !prefs.sound })} />
        </div>
        {/* Drawn as branches of the first rather than as a rule you discover by
            watching three switches grey out together. */}
        <div className="lrow subrow" data-off={active ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("notifications.whenTheTurnEnds")}</span>
            <span className="ds">{t("notifications.theAnswerIsWrittenOr")}</span>
          </span>
          <Switch data-action="notify.turn-done" on={prefs.turnDone} busy={!active} label={t("notifications.whenTheTurnEnds")} onClick={() => flip({ turnDone: !prefs.turnDone })} />
        </div>
        <div className="lrow subrow" data-off={active ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("common.whenSomethingWaitsForApproval")}</span>
            <span className="ds">{t("common.theModelWantsToTouch")}</span>
          </span>
          <Switch data-action="notify.approval" on={prefs.approval} busy={!active} label={t("common.whenSomethingWaitsForApproval")} onClick={() => flip({ approval: !prefs.approval })} />
        </div>
        <div className="lrow subrow" data-off={active ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("common.whenTheModelAsksYou")}</span>
            <span className="ds">{t("common.theModelStoppedToAsk")}</span>
          </span>
          <Switch data-action="notify.ask" on={prefs.ask} busy={!active} label={t("common.whenTheModelAsksYou")} onClick={() => flip({ ask: !prefs.ask })} />
        </div>
        <div className="lrow subrow" data-off={active ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("notifications.whenFeedbackGetsAReply")}</span>
            <span className="ds">{t("notifications.oneAlertWhenAReport")}</span>
          </span>
          <Switch data-action="notify.feedback-reply" on={prefs.feedbackReply} busy={!active} label={t("notifications.whenFeedbackGetsAReply")} onClick={() => flip({ feedbackReply: !prefs.feedbackReply })} />
        </div>
        <div className="lrow subrow" data-off={active ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("notifications.whenDiskSpaceIsLow")}</span>
            <span className="ds">{t("notifications.warnsWhenTheDriveHolding")}</span>
          </span>
          <Switch data-action="notify.disk-low" on={prefs.diskLow} busy={!active} label={t("notifications.whenDiskSpaceIsLow")} onClick={() => flip({ diskLow: !prefs.diskLow })} />
        </div>
      </div>
      <ApplyNote id="notify" />
    </section>
  );
}
