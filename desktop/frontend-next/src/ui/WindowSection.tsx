import { useCallback, useEffect, useState } from "react";
import type { AgentPort, TrayPrefs } from "../port/port";
import { t } from "../i18n";
import { keepsAwake, setKeepsAwake } from "../state/prefs";
import { ApplyNote } from "./Group";
import { Switch } from "./Switch";

export function WindowSection({ port }: { port: AgentPort }) {
  // null in a browser tab, where there is no window to keep running and no
  // icon to bring one back. The whole section goes with it.
  const [tray, setTray] = useState<TrayPrefs | null>(null);
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
        <h3>{t("common.window")}</h3>
      </div>
      <p className="hint">
        {t("settings.after_the_window_closes_the")}
      </p>
      <div className="grp-items">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("settings.keep_the_computer_awake_while")}</span>
            <span className="ds">{t("settings.while_a_session_is_running")}</span>
          </span>
          <Switch
            data-action="chrome.keep-awake"
            on={awake}
            label={t("settings.keep_the_computer_awake_while")}
            onClick={() => {
              setKeepsAwake(!awake);
              setAwake(!awake);
            }}
          />
        </div>
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("settings.show_a_status_icon_in")}</span>
            <span className="ds">
              {tray.icon === tray.live
                ? t("settings.running_or_waiting_on_you")
                : tray.icon
                  ? t("settings.appears_on_the_next_launch")
                  : t("settings.gone_from_the_next_launch")}
            </span>
          </span>
          <Switch data-action="tray.icon" on={tray.icon} label={t("settings.show_a_status_icon_in")} onClick={() => flipTray({ icon: !tray.icon })} />
        </div>
        {/* The second switch only means anything while there is an icon,
            so it is drawn as a branch of the first rather than as a rule
            you have to discover by watching it grey out. */}
        <div className="lrow subrow" data-off={tray.icon && tray.live ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("settings.keep_running_in_the_tray")}</span>
            <span className="ds">{t("settings.closing_the_window_does_not")}</span>
          </span>
          <Switch
            data-action="tray.close-to-tray"
            on={tray.closeToTray}
            busy={!tray.icon || !tray.live}
            label={t("settings.keep_running_in_the_tray")}
            onClick={() => flipTray({ closeToTray: !tray.closeToTray })}
          />
        </div>
      </div>
      <ApplyNote id="window" />
    </section>
  );
}
