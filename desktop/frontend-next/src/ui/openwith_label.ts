import { t } from "../i18n";
import type { OpenWithTarget } from "../port/hub";

export function openWithName(target: OpenWithTarget): string {
  if (target.id === "file-manager") return t("文件管理器");
  if (target.id === "terminal") return t("终端");
  return target.name;
}

/** An installed program the kernel keeps off the list because it sits in a
 *  project folder, which is named so the person can see why. */
export const blockedInProject = (target: OpenWithTarget): boolean => !target.available && target.why === "inside_project";

export const blockedNote = (target: OpenWithTarget): string =>
  t("位于项目文件夹 {path} 内，出于安全不从这里启动", { path: target.within ?? "" });
