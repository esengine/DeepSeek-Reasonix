import { useEffect, useState } from "react";
import type { HubPort, OpenWithTarget, TreeWorkspace } from "../port/hub";
import { host } from "../port/host";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { blockedInProject, blockedNote, openWithName } from "./openwith_label";

export function WorkspaceReveal({ ws, hub, dismiss, onError }: {
  ws: TreeWorkspace;
  hub: HubPort;
  dismiss: () => void;
  onError: (e: unknown) => void;
}) {
  const shell = host().revealsFiles();
  const [targets, setTargets] = useState<OpenWithTarget[]>([]);
  // Asked when the menu opens: what is installed can change while the window
  // stays open, and the kernel answers from the machine as it is now.
  useEffect(() => {
    if (!shell) return;
    let alive = true;
    void hub.openWithTargets().then((list) => alive && setTargets(list.filter((x) => (x.available || blockedInProject(x)) && x.id !== "file-manager"))).catch(() => {});
    return () => { alive = false; };
  }, [hub, shell]);
  if (!shell) return null;
  return (
    <div className="session-pop-group">
      <button
        role="menuitem"
        data-action="workspace.reveal"
        data-target={ws.root}
        disabled={ws.missing}
        onClick={() => {
          dismiss();
          hub.revealWorkspace(ws.root).catch(onError);
        }}
      >
        <StudioIcon name="reveal" /><span>{t("在文件管理器中显示")}</span>
        {ws.missing && <small>{t("文件夹已不在磁盘上")}</small>}
      </button>
      {targets.map((target) => (
        <button
          key={target.id}
          role="menuitem"
          data-action="workspace.open-with"
          data-target={ws.root}
          data-value={target.id}
          disabled={ws.missing || !target.available}
          title={target.available ? undefined : blockedNote(target)}
          onClick={() => {
            dismiss();
            hub.openWith(ws.root, target.id).catch(onError);
          }}
        >
          <StudioIcon name="external" /><span>{t("用 {name} 打开", { name: openWithName(target) })}</span>
          {!target.available && <small>{t("位于项目文件夹内")}</small>}
        </button>
      ))}
    </div>
  );
}
