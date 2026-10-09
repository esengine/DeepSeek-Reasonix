import { t } from "../i18n";
import type { UpdateProgress } from "../port/port";

// What a failed move means and what to do about it, keyed by the code the step
// that failed attached. The kernel's own sentence rides along as detail: it is
// what a bug report needs, and not what a person reads first.
export type FailureCopy = { title: string; why: string; manual: boolean };

export function failureCopy(p: UpdateProgress, current: string): FailureCopy {
  const v = p.version;
  switch (p.code) {
    case "update.catalog_unreachable":
      return { title: t("无法读取 {v} 的发布信息", { v }), why: t("版本目录暂时连不上，可能是网络或代理问题。当前版本未被改动，稍后重试即可。"), manual: false };
    case "update.download_failed":
      return { title: t("{v} 下载中断", { v }), why: t("网络中断或超时。当前版本未被改动，可以重试。"), manual: false };
    case "update.signature_invalid":
      return { title: t("{v} 未通过签名校验", { v }), why: t("下载到的文件与发布签名不符，已丢弃，没有安装。多为下载损坏或网络被篡改，请重试；反复出现请反馈。"), manual: false };
    case "update.disk":
      return { title: t("无法写入更新文件"), why: t("磁盘空间不足，或更新缓存目录不可写。清理空间后重试。"), manual: false };
    case "update.no_package":
      return { title: t("{v} 没有适用于本机的安装包", { v }), why: t("这个版本没有为本机的系统发布可自动安装的包，请下载安装包手动安装。"), manual: true };
    case "update.installer_failed":
      return { title: t("{v} 的安装没能开始", { v }), why: t("安装程序被系统或安全软件拦下了。当前版本未被改动，可以重试，或下载完整安装包手动安装。"), manual: true };
    case "update.not_applied":
      return {
        title: t("上次更新到 {v} 没有生效", { v }),
        why: t("重启后运行的仍是 {current}：新版本没能替换已安装的文件，已恢复原版本。常见原因是装在 Program Files 或其他磁盘，或文件被安全软件占用。下载完整安装包安装一次即可，之后的更新会恢复正常。", { current }),
        manual: true,
      };
  }
  return { title: t("切换到 {v} 失败", { v }), why: t("当前版本未被改动，可以重试。"), manual: false };
}

// Why this move downloads the whole package instead of only what changed,
// keyed by the code the kernel attached when it gave the delta up.
export function deltaSkippedCopy(code: string): string {
  switch (code) {
    case "update.delta.not_swappable":
      return t("安装目录无法就地替换文件，本次改为下载完整安装包。");
    case "update.delta.fetch_failed":
      return t("增量数据下载失败，本次改为下载完整安装包。");
    case "update.delta.timed_out":
      return t("增量数据下载太慢，本次改为下载完整安装包。");
    case "update.delta.mismatch":
      return t("增量数据未通过校验，本次改为下载完整安装包。");
    case "update.delta.too_large":
      return t("这次版本跨度较大，增量数据比完整安装包还多，本次改为下载完整安装包。");
    case "update.delta.disk":
      return t("无法写入增量更新文件，本次改为下载完整安装包。");
  }
  return t("增量更新不可用，本次改为下载完整安装包。");
}

// The release page carries every platform's full installer.
export function releasePage(v: string): string {
  return `https://github.com/esengine/DeepSeek-Reasonix/releases/tag/studio-v${v.replace(/^v/, "")}`;
}
