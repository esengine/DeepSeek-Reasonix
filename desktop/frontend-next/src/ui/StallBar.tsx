import { t } from "../i18n";
import type { Stall } from "../state/session_types";

export type StallAction = "stop" | "mute" | "continue" | "dismiss";

// The kernel's reading that the run stopped producing observable effects, said
// beside the composer. It never takes focus and never opens anything: while the
// run goes on it offers to stop it or to stop saying so; once the user's pause
// setting has ended the run, it offers to go on — a new message, which is what
// zeroes the count — or to leave it stopped.
export function StallBar({ stall, onAct }: { stall: Stall; onAct: (a: StallAction) => void }) {
  const said = stall.cause === "tokens"
    ? t("自上次有进展以来已用约 {n} 倍上下文的输入 token", { n: stall.tokenMultiple })
    : stall.cause === "perseveration"
      ? t("模型在重复输出同一段文字")
      : t("已连续 {n} 轮没有可观察的进展", { n: stall.idleRounds });
  const why = stall.cause === "tokens"
    ? t("共 {n} tokens", { n: stall.promptTokens.toLocaleString() })
    : stall.cause === "perseveration"
      ? t("同一段内容被逐字重复")
      : t("没有文件改动、检查变化或新读取");
  return (
    <div className="rtbar stallbar" data-lvl="warn" role="status" data-paused={stall.paused ? "" : undefined}>
      <span className="t">{stall.paused ? t("任务已暂停：{s}", { s: said }) : said}</span>
      <span className="why" title={why}>{why}</span>
      {stall.paused ? (
        <>
          <button data-action="stall.continue" onClick={() => onAct("continue")}>{t("继续（计数清零）")}</button>
          <button data-action="stall.dismiss" onClick={() => onAct("dismiss")}>{t("停止")}</button>
        </>
      ) : (
        <>
          <button data-action="stall.stop" onClick={() => onAct("stop")}>{t("停止")}</button>
          <button data-action="stall.mute" onClick={() => onAct("mute")}>{t("不再提示（本会话）")}</button>
        </>
      )}
    </div>
  );
}
