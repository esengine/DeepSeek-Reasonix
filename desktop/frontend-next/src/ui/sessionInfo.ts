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
    `${t("session.session_id")}: ${session.name}`,
    `${t("session.session_context_path")}: ${parentPath(session.path)}`,
    `${t("session.task_path")}: ${taskPath}`,
    `${t("session.task_log")}: ${session.path}`,
  ].join("\n");
}
