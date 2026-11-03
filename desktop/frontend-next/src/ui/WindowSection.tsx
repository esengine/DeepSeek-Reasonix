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
        {t("windowSection.afterTheWindowClosesThe")}
      </p>
      <div className="grp-items">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("windowSection.keepTheComputerAwakeWhile")}</span>
            <span className="ds">{t("windowSection.whileASessionIsRunning")}</span>
          </span>
          <Switch
            data-action="chrome.keep-awake"
            on={awake}
            label={t("windowSection.keepTheComputerAwakeWhile")}
            onClick={() => {
              setKeepsAwake(!awake);
              setAwake(!awake);
            }}
          />
        </div>
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("windowSection.showAStatusIconIn")}</span>
            <span className="ds">
              {tray.icon === tray.live
                ? t("windowSection.runningOrWaitingOnYou")
                : tray.icon
                  ? t("windowSection.appearsOnTheNextLaunch")
                  : t("windowSection.goneFromTheNextLaunch")}
            </span>
          </span>
          <Switch data-action="tray.icon" on={tray.icon} label={t("windowSection.showAStatusIconIn")} onClick={() => flipTray({ icon: !tray.icon })} />
        </div>
        {/* The second switch only means anything while there is an icon,
            so it is drawn as a branch of the first rather than as a rule
            you have to discover by watching it grey out. */}
        <div className="lrow subrow" data-off={tray.icon && tray.live ? undefined : ""}>
          <span className="tx">
            <span className="lb">{t("windowSection.keepRunningInTheTray")}</span>
            <span className="ds">{t("windowSection.closingTheWindowDoesNot")}</span>
          </span>
          <Switch
            data-action="tray.close-to-tray"
            on={tray.closeToTray}
            busy={!tray.icon || !tray.live}
            label={t("windowSection.keepRunningInTheTray")}
            onClick={() => flipTray({ closeToTray: !tray.closeToTray })}
          />
        </div>
      </div>
      <ApplyNote id="window" />
    </section>
  );
}
