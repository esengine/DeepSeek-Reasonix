import { useState } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import "../styles/workspace-image.css";

export const isWorkspaceVideo = (path: string) => /\.(mp4|m4v|mov|webm)$/i.test(path);

export function WorkspaceVideo({ port, path }: { port: AgentPort; path: string }) {
  const [failed, setFailed] = useState(false);
  return <>
    <div className="workbench-filebar"><strong title={path}>{path}</strong></div>
    <div className="workbench-image">
      {failed ? <div className="workbench-error" role="alert">{t("视频加载失败，它可能已被移动、删除，或不是可播放的格式")}</div>
        : <video src={port.workspaceVideoURL(path)} controls preload="metadata" aria-label={path} onError={() => setFailed(true)} />}
    </div>
  </>;
}
