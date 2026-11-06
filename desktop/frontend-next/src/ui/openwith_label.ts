import { t } from "../i18n";
import type { OpenWithTarget } from "../port/hub";

export function openWithName(target: OpenWithTarget): string {
  if (target.id === "file-manager") return t("workbench.file_manager");
  if (target.id === "terminal") return t("workbench.terminal");
  // The board writes the editor's short name, which is what the menu has room for.
  if (target.id === "vscode") return "VS Code";
  return target.name;
}

/** An installed program the kernel keeps off the list because it sits in a
 *  project folder, which is named so the person can see why. */
export const blockedInProject = (target: OpenWithTarget): boolean => !target.available && target.why === "inside_project";

export const blockedNote = (target: OpenWithTarget): string =>
  t("workbench.inside_the_project_folder_path", { path: target.within ?? "" });
