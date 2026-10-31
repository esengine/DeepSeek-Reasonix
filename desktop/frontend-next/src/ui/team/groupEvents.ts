import { t } from "../../i18n";
import { say } from "../../i18n/kernel";
import type { GroupLine } from "../../port/team";

// What an event line in a group's log says, named for the member it is about:
// the chat draws it in the log, the sidebar as the group's latest line.
export function eventText(l: GroupLine, name: string): string {
  switch (l.event) {
    case "memory": return l.text ?? t("共享记忆已更新");
    case "passed": return t("{name} 没有补充", { name });
    case "missed": return t("{name} 超时未回应，轮到下一位", { name });
    case "working": return t("{name} 转入后台继续工作", { name });
    case "finished": return t("{name} 完成了工作", { name });
    case "failed": {
      const why = l.code ? say({ code: l.code }) : "";
      return why ? t("{name} 这一轮失败了：{why}", { name, why }) : t("{name} 这一轮失败了", { name });
    }
    case "paused": return t("Agent 之间已接力 {n} 次，已暂停", { n: l.count ?? 0 });
    case "resumed": return t("已继续");
    case "stopped": return t("你停止了 {name}", { name });
    case "cut": return t("{name} 的发言被中断", { name });
  }
  return "";
}
