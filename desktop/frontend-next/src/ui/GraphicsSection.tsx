import { useEffect, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { host, type GraphicsInfo } from "../port/host";
import { onSoftwareRenderingChange, setSoftwareRendering, wantsSoftwareRendering } from "../state/prefs";
import { Group } from "./Group";

function modeSaid(g: GraphicsInfo): string {
  if (g.launchedOff) return t("common.softwareRendering") + " · " + t(g.savedOff === false ? "graphicsSection.hardwareAccelerationWasSwitchedOff" : "graphicsSection.hardwareAccelerationIsSwitchedOff");
  return g.compositing ? t("graphicsSection.graphicsCompositingStatus", { value: g.compositing }) : t("graphicsSection.noGraphicsStatusReportedYet");
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
      title={t("common.graphics")}
      hint={t("graphicsSection.ifTheWindowKeepsYour")}
    >
      <div className="seg" data-text role="group" aria-label={t("common.graphics")}>
        {([false, true] as const).map((off) => (
          <button key={String(off)} data-action="appearance.graphics" data-value={off ? "software" : "auto"} aria-pressed={software === off} onClick={() => setSoftwareRendering(off)}>
            {t(off ? "graphicsSection.softwareOnly" : "graphicsSection.systemDefault")}
          </button>
        ))}
      </div>
      <p className="note" data-graphics={now.launchedOff ? "software" : now.compositing ? "reported" : "unreported"}>
        {t("graphicsSection.thisLaunchUses", { mode: modeSaid(now) })}
      </p>
      {pending && <p className="note" role="status">{t("graphicsSection.savedQuitAndReopenStudio")}</p>}
    </Group>
  );
}
