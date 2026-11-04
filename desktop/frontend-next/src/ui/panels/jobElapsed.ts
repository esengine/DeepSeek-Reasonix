import { t } from "../../i18n";

export function jobElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  if (total < 60) return t("common.n_s", { n: total });
  const minutes = Math.floor(total / 60);
  if (minutes < 60) {
    const seconds = total % 60;
    return seconds ? t("common.m_m_s_s", { m: minutes, s: String(seconds).padStart(2, "0") }) : t("panels.m_min", { m: minutes });
  }
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest ? t("common.h_h_m_m", { h: hours, m: rest }) : t("panels.h_h", { h: hours });
}
