import type { Item } from "../../state/session";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import type { ApprovalVerdict } from "../../port/port";
import { useState } from "react";

// The Tool name the kernel puts on a plan gate; its own comment says frontends
// key their plan UI on it. `kind` says the same thing on newer kernels.
const PLAN_TOOL = "exit_plan_mode";
// Widening a delegated run's write confinement. Not a tool the run calls: the
// answer moves the fence rather than performing anything, so the card says that
// instead of "about to run extend_write_paths".
const FENCE_TOOL = "extend_write_paths";

import type { PlanAction } from "../../port/session";
export type { PlanAction };

interface Props {
  item: Extract<Item, { t: "approval" }>;
  onApprove: (itemId: string, id: string, v: ApprovalVerdict) => Promise<void>;
  onPlan: (itemId: string, id: string, action: PlanAction) => Promise<void>;
}

// The run is genuinely blocked here until Approve() resolves it, so this card
// must be the only way past — no other control may advance the queue.
export function ApprovalCard({ item, onApprove, onPlan }: Props) {
  const sealed = item.verdict !== undefined;
  const [submitting, setSubmitting] = useState<ApprovalVerdict | "">("");
  const decide = async (verdict: ApprovalVerdict) => {
    if (submitting) return;
    setSubmitting(verdict);
    try {
      await onApprove(item.id, item.a.id, verdict);
    } finally {
      setSubmitting("");
    }
  };
  if (item.a.kind === "plan" || item.a.tool === PLAN_TOOL) return <PlanGate item={item} onPlan={onPlan} />;
  return (
    <div className="call" data-k="ask">
      <div className="g">
        <Sym glyph="?" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{item.a.tool === FENCE_TOOL ? t("要求扩大可改范围") : t("即将执行")}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined} aria-busy={!!submitting}>
            <div className="apv-hd">
              <span className="tool">{item.a.tool === FENCE_TOOL ? t("这个子任务声明之外的文件") : item.a.tool}</span>
              <span className="sub" title={item.a.subject}>{item.a.subject}</span>
            </div>
            {item.a.reason && <div className="apv-dt">{item.a.reason}</div>}
            {!sealed && (
              <div className="apv-ft">
                <button className="btn" data-primary data-action="decision.tool" data-target={item.a.id} data-value="once"
                  disabled={!!submitting} onClick={() => void decide("once")}>
                  {submitting === "once" ? t("正在提交…") : t("允许这一次")}
                </button>
                <button className="btn" data-action="decision.tool" data-target={item.a.id} data-value="always"
                  disabled={!!submitting} onClick={() => void decide("always")}>
                  {t("此类操作不再询问")}
                </button>
                <button className="btn" data-action="decision.tool" data-target={item.a.id} data-value="deny"
                  disabled={!!submitting} onClick={() => void decide("deny")}>
                  {t("拒绝")}
                </button>
              </div>
            )}
            {sealed && (
              <div className="apv-done">
                {item.verdict === "always" ? (
                  <><b>{t("本会话不再询问此类操作。")}</b>{t("内核已记入会话授权，不写入磁盘。")}</>
                ) : item.verdict === "deny" ? (
                  <><b>{t("已拒绝。")}</b>{t("agent 已收到拒绝，将改用其他方式或终止。")}</>
                ) : item.verdict === "persist" ? (
                  <><b>{t("已保存为规则。")}</b>{t("已写入配置，后续会话也不再询问此类操作。")}</>
                ) : item.verdict === "unknown" ? (
                  <><b>{t("已在其他窗口处理。")}</b>{t("请以最新运行状态为准。")}</>
                ) : (
                  <><b>{t("允许这一次。")}</b>{t("下次同样的操作仍会请求确认。")}</>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// A plan is not a tool call, and the generic card said so in the wrong words:
// 「允许这一次 / 此类操作不再询问 / 拒绝」. Two of those are wrong for a plan —
// nothing here is repeatable, so there is no class to stop asking about, and the
// kernel refuses a remembered grant for this gate anyway. And the vocabulary hid
// the outcome people actually wanted: denying keeps planning, which is how you
// change the plan. So the card names the three the kernel really distinguishes
// (control.PlanDecisionAction), including the one Studio never offered.
function PlanGate({ item, onPlan }: { item: Props["item"]; onPlan: Props["onPlan"] }) {
  const sealed = item.verdict !== undefined;
  const [submitting, setSubmitting] = useState<PlanAction | "">("");
  const decide = async (action: PlanAction) => {
    if (submitting) return;
    setSubmitting(action);
    try {
      await onPlan(item.id, item.a.id, action);
    } finally {
      setSubmitting("");
    }
  };
  const said =
    item.verdict === "start"
      ? [t("已开始执行。"), t("计划模式已关闭，进入执行阶段。")]
      : item.verdict === "exit"
        ? [t("暂不执行。"), t("已退出计划模式，计划保留在上方，可随时重新下达。")]
        : item.verdict === "revise" || item.verdict === "deny"
          ? [t("继续规划。"), t("在下方说明需要修改的内容，规划者将据此重写该计划。")]
          : [t("已在其他窗口处理。"), t("请以最新运行状态为准。")];
  return (
    <div className="call" data-k="ask">
      <div className="g">
        <Sym glyph="?" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("计划待确认")}</span>
          <span className="tag">{item.a.tool}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined} aria-busy={!!submitting}>
            <div className="apv-dt">{item.a.reason || t("计划已生成，如何执行由你决定。")}</div>
            {!sealed && (
              <>
                <div className="apv-ft">
                  <button className="btn" data-primary data-action="decision.plan" data-target={item.a.id} data-value="start"
                    disabled={!!submitting} onClick={() => void decide("start")}>
                    {submitting === "start" ? t("正在提交…") : t("开始执行")}
                  </button>
                  <button className="btn" data-action="decision.plan" data-target={item.a.id} data-value="revise"
                    disabled={!!submitting} onClick={() => void decide("revise")}>
                    {t("修改计划")}
                  </button>
                  <button className="btn" data-action="decision.plan" data-target={item.a.id} data-value="exit"
                    disabled={!!submitting} onClick={() => void decide("exit")}>
                    {t("暂不执行")}
                  </button>
                </div>
                {/* 这一句就是这张卡存在的理由：过去只有「拒绝」，而它其实是
                    「继续规划」—— 想改计划的人以为自己把计划扔了。 */}
                <div className="apv-note">{t("「修改计划」将保持在计划模式中，请在下方说明需要修改之处。")}</div>
              </>
            )}
            {sealed && (
              <div className="apv-done">
                <b>{said[0]}</b>
                {said[1]}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
