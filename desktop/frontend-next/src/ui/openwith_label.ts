import { t } from "../i18n";
import type { OpenWithTarget } from "../port/hub";

export function openWithName(target: OpenWithTarget): string {
  if (target.id === "file-manager") return t("openwithLabel.fileManager");
  if (target.id === "terminal") return t("openwithLabel.terminal");
  return target.name;
}

/** An installed program the kernel keeps off the list because it sits in a
 *  project folder, which is named so the person can see why. */
export const blockedInProject = (target: OpenWithTarget): boolean => !target.available && target.why === "inside_project";

export const blockedNote = (target: OpenWithTarget): string =>
  t("openwithLabel.insideTheProjectFolderSo", { path: target.within ?? "" });
