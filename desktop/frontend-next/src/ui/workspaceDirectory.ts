import { t } from "../i18n";

export function workspaceDirectoryDescription(isolated: boolean): string {
  return isolated
    ? t("workspaceDirectory.editCodeInASeparate")
    : t("workspaceDirectory.workDirectlyInTheOpened");
}
