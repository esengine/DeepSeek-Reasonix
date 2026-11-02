import { t } from "../i18n";

export function workspaceDirectoryDescription(isolated: boolean): string {
  return isolated
    ? t("在单独的 Git 工作目录中修改代码；端口、数据库等运行资源仍可能共享。")
    : t("直接在已打开的项目文件夹中工作，与使用该目录的其他会话共享文件。");
}
