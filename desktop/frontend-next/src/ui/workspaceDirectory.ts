import { t } from "../i18n";

export function workspaceDirectoryDescription(isolated: boolean): string {
  return isolated
    ? t("session.edit_code_in_a_separate")
    : t("session.work_directly_in_the_opened");
}
