import { useEffect, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { host, type GraphicsInfo } from "../port/host";
import { onSoftwareRenderingChange, setSoftwareRendering, wantsSoftwareRendering } from "../state/prefs";
import { Group } from "./Group";

function modeSaid(g: GraphicsInfo): string {
  if (g.launchedOff) return t("软件渲染") + " · " + t("已按你的设置关闭硬件加速");
  if (g.compositing && g.compositing !== "enabled") return t("软件渲染") + " · " + t("驱动不可用，已由系统退回软件渲染");
  return t("硬件加速");
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
  const pending = software !== now.launchedOff;
  return (
    <Group
      id="graphics"
      title={t("图形渲染")}
      hint={t("窗口一直占用显卡、或界面花屏时，先试「视觉效果：节能」，不行再改为「仅软件渲染」：改用 CPU 绘制，显卡不再参与。设置保存后，需退出并重新打开 Studio 才会生效。")}
    >
      <div className="seg" data-text role="group" aria-label={t("图形渲染")}>
        {([false, true] as const).map((off) => (
          <button key={String(off)} data-action="appearance.graphics" data-value={off ? "software" : "auto"} aria-pressed={software === off} onClick={() => setSoftwareRendering(off)}>
            {t(off ? "仅软件渲染" : "随系统")}
          </button>
        ))}
      </div>
      <p className="note" data-graphics={now.launchedOff ? "software" : now.compositing === "enabled" ? "hardware" : "fallback"}>
        {t("当前启动使用：{mode}", { mode: modeSaid(now) })}
      </p>
      {pending && <p className="note" role="status">{t("已保存，退出并重新打开 Studio 后生效")}</p>}
    </Group>
  );
}
