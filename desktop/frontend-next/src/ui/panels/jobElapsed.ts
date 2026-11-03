import { t } from "../../i18n";

export function jobElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  if (total < 60) return t("{n} 秒", { n: total });
  const minutes = Math.floor(total / 60);
  if (minutes < 60) {
    const seconds = total % 60;
    return seconds ? t("{m} 分 {s} 秒", { m: minutes, s: seconds }) : t("{m} 分钟", { m: minutes });
  }
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest ? t("{h} 小时 {m} 分", { h: hours, m: rest }) : t("{h} 小时", { h: hours });
}
