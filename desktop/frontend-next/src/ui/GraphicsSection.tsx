import { useEffect, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { host, type GraphicsInfo } from "../port/host";
import { onSoftwareRenderingChange, setSoftwareRendering, wantsSoftwareRendering } from "../state/prefs";
import { Group } from "./Group";

function modeSaid(g: GraphicsInfo): string {
  if (g.launchedOff) return t("look.software_rendering") + " · " + t(g.savedOff === false ? "look.hardware_acceleration_was_switched_off" : "look.hardware_acceleration_is_switched_off");
  return g.compositing ? t("look.graphics_compositing_status_value", { value: g.compositing }) : t("look.no_graphics_status_reported_yet");
}

/** Which way the window is drawn this launch, and the one switch the shell can
 *  honour: Chromium fixes its GPU use before the app is ready, so a change is
 *  saved now and applies on the next launch. Absent in a browser tab. */
export function useShellGraphics(): GraphicsInfo | null {
  const [now, setNow] = useState<GraphicsInfo | null>(null);
  useEffect(() => {
    let live = true;
    host().graphics().then((g) => live && setNow(g)).catch(() => live && setNow(null));
    return () => {
      live = false;
    };
  }, []);
  return now;
}

export function GraphicsSection() {
  const now = useShellGraphics();
  const software = useSyncExternalStore(onSoftwareRenderingChange, wantsSoftwareRendering, wantsSoftwareRendering);
  if (!now) return null;
  const pending = software !== (now.savedOff ?? now.launchedOff);
  return (
    <Group
      id="graphics"
      title={t("look.graphics")}
      hint={t("look.if_the_window_keeps_your")}
    >
      <div className="seg" data-text role="group" aria-label={t("look.graphics")}>
        {([false, true] as const).map((off) => (
          <button key={String(off)} data-action="appearance.graphics" data-value={off ? "software" : "auto"} aria-pressed={software === off} onClick={() => setSoftwareRendering(off)}>
            {t(off ? "look.software_only" : "look.system_default")}
          </button>
        ))}
      </div>
      <p className="note" data-graphics={now.launchedOff ? "software" : now.compositing ? "reported" : "unreported"}>
        {t("look.this_launch_uses_mode", { mode: modeSaid(now) })}
      </p>
      {pending && <p className="note" role="status">{t("look.saved_quit_and_reopen_studio")}</p>}
    </Group>
  );
}
