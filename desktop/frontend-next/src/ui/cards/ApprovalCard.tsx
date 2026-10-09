import type { Item } from "../../state/session";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import type { ApprovalVerdict } from "../../port/port";
import { useState } from "react";
import { decisionSource } from "../source";
import { useViewer } from "../../state/viewer";

// The Tool name the kernel puts on a plan gate; its own comment says frontends
// key their plan UI on it. `kind` says the same thing on newer kernels.
const PLAN_TOOL = "exit_plan_mode";
// Widening a delegated run's write confinement. Not a tool the run calls: the
// answer moves the fence rather than performing anything, so the card says that
// instead of "about to run extend_write_paths".
const FENCE_TOOL = "extend_write_paths";

// Why no posture answers for this call. The kernel names the class and writes
// the sentence it gave the model; a reader gets that class in their own
// language, and an unknown one falls back to what the model was told.
const EXPLICIT_APPROVAL: Record<string, string> = {
  dynamic_bash: "这条命令里嵌套或间接地执行了别的命令。自动放行和宽泛的允许规则都无法确认真正会跑的是什么；要么批准这一条，要么切到全部放行。",
  browser_credential: "这一步会把密码、一次性验证码或银行卡信息输入网站。自动放行、该站点已有的授权和宽泛规则都不能代你回答；要么批准，要么切到全部放行。",
  computer_use: "这会读取或操作电脑上的另一个应用，拿到的是那个应用本身的权限。自动放行和宽泛规则都不能代你回答；要么为这个应用批准，要么切到全部放行。",
  computer_front: "这会操作电脑上的另一个应用，拿到的是那个应用本身的权限。Windows 只把按键送给最前面的窗口，所以输入文字、按键和粘贴会先把这个应用切到前台。自动放行和宽泛规则都不能代你回答；要么为这个应用批准，要么切到全部放行。",
  computer_pointer: "这一步会接管你正在用的鼠标指针——移动它、用它点击，并把那个应用切到前台，而不是让界面元素自己动作。自动放行、该应用已有的授权和宽泛规则都不能代你回答；要么批准，要么切到全部放行。用完指针会回到原处。",
};

const TASK_PREVIEW = 280;

// What approving authorizes beyond this one call, named by the kernel. A scope
// this window does not know still says that one exists rather than nothing.
function ApprovalScope({ scope, input }: { scope: string; input?: Record<string, unknown> }) {
  if (scope === "unattended_attempts") {
    const n = typeof input?.n === "number" ? input.n : undefined;
    const task = typeof input?.prompt === "string" ? input.prompt.trim() : "";
    return (
      <>
        <div className="apv-dt" data-scope={scope}>
          <span className="apv-dt-label">{t("批准后还会发生")}</span>
          <span>
            {n ? t("并行启动 {n} 个候选", { n }) : t("并行启动多个候选")}
            {t("，各自在独立的 git 工作副本里无人值守地运行：它们的工具调用按「自动审核」处理，不再逐个询问（拒绝规则仍然生效；会话为「不询问」时候选也不询问）。最后由评审选出一个写回工作区。")}
          </span>
        </div>
        {task && (
          <div className="apv-dt">
            <span className="apv-dt-label">{t("候选要做的任务")}</span>
            <span title={task}>{task.length > TASK_PREVIEW ? task.slice(0, TASK_PREVIEW) + "…" : task}</span>
          </div>
        )}
      </>
    );
  }
  return (
    <div className="apv-dt" data-scope={scope}>
      <span className="apv-dt-label">{t("批准后还会发生")}</span>
      <span>{t("这次批准的效力超出这一次调用本身。")}</span>
    </div>
  );
}

function approvalReason(a: { reason?: string; reasonCode?: string }): string {
  if (a.reasonCode && EXPLICIT_APPROVAL[a.reasonCode]) return t(EXPLICIT_APPROVAL[a.reasonCode]);
  // Older kernels only sent the English sentence. Keep that compatibility
  // path readable instead of exposing an implementation message in a Chinese UI.
  if (a.reason?.toLowerCase().includes("nested or indirect shell execution")) {
    return t(EXPLICIT_APPROVAL.dynamic_bash);
  }
  return a.reason || "";
}

import type { PlanAction } from "../../port/session";
export type { PlanAction };

interface Props {
  item: Extract<Item, { t: "approval" }>;
  onApprove: (itemId: string, id: string, v: ApprovalVerdict) => Promise<void>;
  onFullAccess: (itemId: string) => Promise<void>;
  onPlan: (itemId: string, id: string, action: PlanAction) => Promise<void>;
}

// The run is genuinely blocked here until Approve() resolves it, so this card
// must be the only way past — no other control may advance the queue.
export function ApprovalCard({ item, onApprove, onFullAccess, onPlan }: Props) {
  const sealed = item.verdict !== undefined;
  const settledBy = decisionSource(item.by, useViewer());
  const [submitting, setSubmitting] = useState<ApprovalVerdict | "">("");
  const [confirmFullAccess, setConfirmFullAccess] = useState(false);
  const [switchingFullAccess, setSwitchingFullAccess] = useState(false);
  const explicit = !!item.a.reasonCode || item.a.reason?.toLowerCase().includes("nested or indirect shell execution");
  const decide = async (verdict: ApprovalVerdict) => {
    if (submitting) return;
    setSubmitting(verdict);
    try {
      await onApprove(item.id, item.a.id, verdict);
    } finally {
      setSubmitting("");
    }
  };
  const useFullAccess = async () => {
    if (switchingFullAccess) return;
    setSwitchingFullAccess(true);
    try {
      await onFullAccess(item.id);
    } finally {
      setSwitchingFullAccess(false);
    }
  };
  if (item.a.kind === "plan" || item.a.tool === PLAN_TOOL) return <PlanGate item={item} onPlan={onPlan} />;
  return (
    // 咨询与授权此前共用 data-k="ask" 和同一个「?」：一个是模型想听你的意见，
    // 另一个是它要动你的文件。授权借「写」的类别 —— 它本来就是一次写权限。
    <div className="call" data-k="write" data-approval={sealed ? item.verdict : "pending"} data-prompt={sealed ? "settled" : "pending"}>
      <div className="g">
        <Sym glyph="⊛" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{item.a.tool === FENCE_TOOL ? t("要求扩大可改范围") : t("请求执行权限")}</span>
          <span className="tag">{sealed ? t("已处理") : t("等待确认")}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined} aria-busy={!!submitting}>
            <div className="apv-hd">
              <span className="tool">{item.a.tool === FENCE_TOOL ? t("这个子任务声明之外的文件") : item.a.tool}</span>
              <span className="sub" title={item.a.subject}>{item.a.subject}</span>
            </div>
            {item.a.scope && <ApprovalScope scope={item.a.scope} input={item.a.input} />}
            {approvalReason(item.a) && (
              <div className="apv-dt">
                <span className="apv-dt-label">{t("需要确认的原因")}</span>
                <span>{approvalReason(item.a)}</span>
              </div>
            )}
            {!sealed && (
              <div className="apv-ft">
                <button className="btn" data-primary data-action="decision.tool" data-target={item.a.id} data-value="once"
                  disabled={!!submitting} onClick={() => void decide("once")}>
                  {submitting === "once" ? t("正在提交…") : t("允许这一次")}
                </button>
                {/* 后两颗比「这一次」安静一档：三颗同样大小挨在一起时，读者分不出
                    哪颗只管这一次、哪颗管到会话结束、哪颗写进配置。内核会说这次
                    认哪几种授权，不认的就不画 —— 画出来就是承诺它做不到的事。 */}
                {item.a.allowsSession && (
                  <button className="btn" data-weak="" data-action="decision.tool" data-target={item.a.id} data-value="session"
                    disabled={!!submitting} onClick={() => void decide("session")}>
                    {t("本会话允许此类操作，不再询问")}
                  </button>
                )}
                {item.a.allowsPersist && (
                  <button className="btn" data-weak="" data-action="decision.tool" data-target={item.a.id} data-value="always"
                    disabled={!!submitting} onClick={() => void decide("always")}>
                    {t("始终允许此类操作，不再询问")}
                  </button>
                )}
                <button className="btn" data-deny="" data-action="decision.tool" data-target={item.a.id} data-value="deny"
                  disabled={!!submitting} onClick={() => void decide("deny")}>
                  {t("拒绝")}
                </button>
                {explicit && (
                  <button className="btn" data-weak="" data-yolo="" data-action="decision.full-access"
                    disabled={!!submitting} onClick={() => setConfirmFullAccess(true)}>
                    {t("切换全部放行…")}
                  </button>
                )}
              </div>
            )}
            {!sealed && confirmFullAccess && (
              <div className="apv-yolo-confirm" role="alertdialog" aria-label={t("确认切换全部放行")}>
                <div><b>{t("全部放行会跳过后续工具确认")}</b><span>{t("拒绝规则与沙盒边界仍然生效。仅在完全信任当前工作区时使用。")}</span></div>
                <div>
                  <button className="btn" disabled={switchingFullAccess} onClick={() => setConfirmFullAccess(false)}>{t("返回")}</button>
                  <button className="btn" data-yolo-confirm="" disabled={switchingFullAccess} onClick={() => void useFullAccess()}>
                    {switchingFullAccess ? t("正在切换…") : t("确认全部放行")}
                  </button>
                </div>
              </div>
            )}
            {sealed && (
              <div className="apv-done">
                {item.verdict === "session" ? (
                  <><b>{t("已允许：本会话内此类操作不再询问。")}</b>{t("内核已记入会话授权，不写入磁盘。")}</>
                ) : item.verdict === "always" ? (
                  <><b>{t("已允许：已保存为规则。")}</b>{t("已写入配置，后续会话中此类操作也始终允许，不再询问。")}</>
                ) : item.verdict === "yolo" ? (
                  <><b>{t("已切换全部放行。")}</b>{t("后续工具不再请求确认；拒绝规则与沙盒边界仍然生效。")}</>
                ) : item.verdict === "deny" ? (
                  <><b>{t("已拒绝。")}</b>{t("agent 已收到拒绝，将改用其他方式或终止。")}</>
                ) : item.verdict === "unknown" ? (
                  <><b>{t("已在其他窗口处理。")}</b>{t("请以最新运行状态为准。")}</>
                ) : (
                  <><b>{t("允许这一次。")}</b>{t("下次同样的操作仍会请求确认。")}</>
                )}
                {settledBy && <span className="apv-by">{settledBy}</span>}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// A plan is not a tool call, and the generic card said so in the wrong words:
// 「允许这一次 / 始终允许此类操作，不再询问 / 拒绝」. Two of those are wrong for a plan —
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
    <div className="call" data-k="ask" data-prompt={sealed ? "settled" : "pending"}>
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
