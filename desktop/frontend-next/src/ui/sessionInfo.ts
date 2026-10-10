import { t } from "../i18n";

interface SessionInfo {
  name: string;
  path: string;
}

function parentPath(path: string): string {
  const at = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return at > 0 ? path.slice(0, at) : path;
}

export function sessionInfoText(session: SessionInfo, taskPath: string): string {
  return [
    `${t("会话 ID")}: ${session.name}`,
    `${t("会话上下文路径")}: ${parentPath(session.path)}`,
    `${t("任务路径")}: ${taskPath}`,
    `${t("任务日志")}: ${session.path}`,
  ].join("\n");
}
