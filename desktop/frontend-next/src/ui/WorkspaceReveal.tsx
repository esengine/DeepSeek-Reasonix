import type { HubPort, TreeWorkspace } from "../port/hub";
import { host } from "../port/host";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function WorkspaceReveal({ ws, hub, dismiss, onError }: {
  ws: TreeWorkspace;
  hub: HubPort;
  dismiss: () => void;
  onError: (e: unknown) => void;
}) {
  if (!host().revealsFiles()) return null;
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
    </div>
  );
}
