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
    `${t("sessionInfo.sessionID")}: ${session.name}`,
    `${t("sessionInfo.sessionContextPath")}: ${parentPath(session.path)}`,
    `${t("sessionInfo.taskPath")}: ${taskPath}`,
    `${t("sessionInfo.taskLog")}: ${session.path}`,
  ].join("\n");
}
