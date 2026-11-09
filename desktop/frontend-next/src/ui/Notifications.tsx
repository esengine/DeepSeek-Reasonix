import { useCallback, useEffect, useState } from "react";
import type { AgentPort, NotifyPrefs } from "../port/port";
import { t } from "../i18n";
import { SettingRow } from "./SettingRow";
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
    <>
      <SettingRow id="notify" title={t("common.notifications")} desc={t("settings.a_system_notification_when_a")}>
        <Switch data-action="notify.enabled" on={prefs.enabled} label={t("settings.send_system_notifications")} onClick={() => flip({ enabled: !prefs.enabled })} />
      </SettingRow>
        <div className="srow-more">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("settings.play_a_sound")}</span>
          <span className="ds">{t("settings.off_by_default_plays_a")}</span>
        </span>
        <Switch data-action="notify.sound" on={prefs.sound} label={t("settings.play_a_sound")} onClick={() => flip({ sound: !prefs.sound })} />
      </div>
      {/* Drawn as branches of the first rather than as a rule you discover by
          watching three switches grey out together. */}
      <div className="lrow subrow" data-off={active ? undefined : ""}>
        <span className="tx">
          <span className="lb">{t("settings.when_the_turn_ends")}</span>
          <span className="ds">{t("settings.the_answer_is_written_or")}</span>
        </span>
        <Switch data-action="notify.turn-done" on={prefs.turnDone} busy={!active} label={t("settings.when_the_turn_ends")} onClick={() => flip({ turnDone: !prefs.turnDone })} />
      </div>
      <div className="lrow subrow" data-off={active ? undefined : ""}>
        <span className="tx">
          <span className="lb">{t("common.when_something_waits_for_approval")}</span>
          <span className="ds">{t("common.the_model_wants_to_touch")}</span>
        </span>
        <Switch data-action="notify.approval" on={prefs.approval} busy={!active} label={t("common.when_something_waits_for_approval")} onClick={() => flip({ approval: !prefs.approval })} />
      </div>
      <div className="lrow subrow" data-off={active ? undefined : ""}>
        <span className="tx">
          <span className="lb">{t("common.when_the_model_asks_you")}</span>
          <span className="ds">{t("common.the_model_stopped_to_ask")}</span>
        </span>
        <Switch data-action="notify.ask" on={prefs.ask} busy={!active} label={t("common.when_the_model_asks_you")} onClick={() => flip({ ask: !prefs.ask })} />
      </div>
      <div className="lrow subrow" data-off={active ? undefined : ""}>
        <span className="tx">
          <span className="lb">{t("settings.when_feedback_gets_a_reply")}</span>
          <span className="ds">{t("settings.one_alert_when_a_report")}</span>
        </span>
        <Switch data-action="notify.feedback-reply" on={prefs.feedbackReply} busy={!active} label={t("settings.when_feedback_gets_a_reply")} onClick={() => flip({ feedbackReply: !prefs.feedbackReply })} />
      </div>
      <div className="lrow subrow" data-off={active ? undefined : ""}>
        <span className="tx">
          <span className="lb">{t("settings.when_disk_space_is_low")}</span>
          <span className="ds">{t("settings.warns_when_the_drive_holding")}</span>
        </span>
        <Switch data-action="notify.disk-low" on={prefs.diskLow} busy={!active} label={t("settings.when_disk_space_is_low")} onClick={() => flip({ diskLow: !prefs.diskLow })} />
      </div>
        </div>
    </>
  );
}
