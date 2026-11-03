import type { Item } from "../../state/session";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import type { ApprovalVerdict } from "../../port/port";
import { useState } from "react";
import { decisionSource } from "../source";
import { RememberChoice, RememberWhere, type Remembered } from "./RememberChoice";
import { useViewer } from "../../state/viewer";

// The Tool name the kernel puts on a plan gate; its own comment says frontends
// key their plan UI on it. `kind` says the same thing on newer kernels.
const PLAN_TOOL = "exit_plan_mode";
// Widening a delegated run's write confinement. Not a tool the run calls: the
// answer moves the fence rather than performing anything, so the card says that
// instead of "about to run extend_write_paths".
const FENCE_TOOL = "extend_write_paths";
// The model asking to change one of the user's settings. The card is worded by
// the kernel; the elevated one offers no session grant, which `allowsSession` already says.
const SETTING_TOOL = "setting_change";
const SETTING_ELEVATED_TOOL = "setting_change_elevated";
// A call that repeats text from outside; the card says so instead of showing the tool id.
const ORIGIN_TOOL = "origin_check";

// Why no posture answers for this call. The kernel names the class and writes
// the sentence it gave the model; a reader gets that class in their own
// language, and an unknown one falls back to what the model was told.
const EXPLICIT_APPROVAL: Record<string, string> = {
  dynamic_bash: "approvalCard.thisCommandRunsAnotherCommand",
  browser_credential: "approvalCard.thisStepTypesAPassword",
  computer_use: "approvalCard.thisReadsOrOperatesAnother",
  computer_front: "approvalCard.thisOperatesAnotherApplicationOn",
  origin_text: "approvalCard.textInThisCallIs",
  computer_pointer: "approvalCard.thisStepTakesThePointer",
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
          <span className="apv-dt-label">{t("approvalCard.approvingAlso")}</span>
          <span>
            {n ? t("approvalCard.startsAttemptsInParallel", { n }) : t("approvalCard.startsSeveralAttemptsInParallel")}
            {t("approvalCard.eachRunningUnattendedInIts")}
          </span>
        </div>
        {task && (
          <div className="apv-dt">
            <span className="apv-dt-label">{t("approvalCard.theAttemptsTask")}</span>
            <span title={task}>{task.length > TASK_PREVIEW ? task.slice(0, TASK_PREVIEW) + "…" : task}</span>
          </div>
        )}
      </>
    );
  }
  if (scope === "open_projects") {
    const paths = Array.isArray(input?.paths) ? input.paths.filter((p): p is string => typeof p === "string") : [];
    return (
      <div className="apv-dt" data-scope={scope}>
        <span className="apv-dt-label">{t("approvalCard.approvingAlso")}</span>
        <span>
          {t("approvalCard.addsTheseFoldersToYour", { n: paths.length })}
          <ul className="apv-paths">{paths.map((p) => <li key={p}><code title={p}>{p}</code></li>)}</ul>
        </span>
      </div>
    );
  }
  return (
    <div className="apv-dt" data-scope={scope}>
      <span className="apv-dt-label">{t("approvalCard.approvingAlso")}</span>
      <span>{t("approvalCard.thisApprovalCoversMoreThan")}</span>
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
  const remembering: Remembered[] = [...(item.a.allowsSession ? ["session" as const] : []), ...(item.a.allowsPersist ? ["always" as const] : [])];
  // Full access cannot answer the origin question, so the card does not offer it.
  const explicit = item.a.tool !== ORIGIN_TOOL && (!!item.a.reasonCode || item.a.reason?.toLowerCase().includes("nested or indirect shell execution"));
  const isSetting = item.a.tool === SETTING_TOOL || item.a.tool === SETTING_ELEVATED_TOOL;
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
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{item.a.tool === FENCE_TOOL ? t("approvalCard.askingToWidenWhatIt") : isSetting ? t("approvalCard.asksToChangeASetting") : t("approvalCard.permissionToRun")}</span>
          <span className="tag">{sealed ? t("approvalCard.handled") : t("approvalCard.waitingOnYou")}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined} aria-busy={!!submitting}>
            <div className="apv-hd" data-whole={item.a.tool === ORIGIN_TOOL ? "" : undefined}>
              <span className="tool">{item.a.tool === FENCE_TOOL ? t("approvalCard.aFileOutsideThisSubtask") : isSetting ? t("common.settings") : item.a.tool === ORIGIN_TOOL ? t("approvalCard.textFromOutside") : item.a.tool}</span>
              <span className="sub" title={item.a.subject}>{item.a.subject}</span>
            </div>
            {item.a.scope && <ApprovalScope scope={item.a.scope} input={item.a.input} />}
            {approvalReason(item.a) && (
              <div className="apv-dt">
                <span className="apv-dt-label">{t("approvalCard.whyConfirmationIsNeeded")}</span>
                <span style={isSetting ? { whiteSpace: "pre-line" } : undefined}>{approvalReason(item.a)}</span>
              </div>
            )}
            {item.a.tool === SETTING_ELEVATED_TOOL && (
              <div className="apv-dt" data-setting="elevated">
                <span className="apv-dt-label">{t("approvalCard.askedEveryTime")}</span>
                <span>{t("approvalCard.thisSettingLoosensWhatThe")}</span>
              </div>
            )}
            {!sealed && (
              <div className="apv-ft">
                <button className="btn" data-primary data-action="decision.tool" data-target={item.a.id} data-value="once"
                  disabled={!!submitting} onClick={() => void decide("once")}>
                  {submitting === "once" ? t("common.submitting") : t("approvalCard.allowOnce")}
                </button>
                {/* 后两颗比「这一次」安静一档：三颗同样大小挨在一起时，读者分不出
                    哪颗只管这一次、哪颗管到会话结束、哪颗写进配置。内核会说这次
                    认哪几种授权，不认的就不画 —— 画出来就是承诺它做不到的事。 */}
                <RememberChoice options={remembering} target={item.a.id} busy={submitting}
                  disabled={!!submitting} onChoose={(choice) => void decide(choice)} />
                <button className="btn" data-deny="" data-action="decision.tool" data-target={item.a.id} data-value="deny"
                  disabled={!!submitting} onClick={() => void decide("deny")}>
                  {t("common.refuse")}
                </button>
                {explicit && (
                  <button className="btn" data-weak="" data-yolo="" data-action="decision.full-access"
                    disabled={!!submitting} onClick={() => setConfirmFullAccess(true)}>
                    {t("approvalCard.switchToFullAccess")}
                  </button>
                )}
              </div>
            )}
            {!sealed && <RememberWhere options={remembering} target={item.a.id} tool={item.a.tool} />}
            {!sealed && confirmFullAccess && (
              <div className="apv-yolo-confirm" role="alertdialog" aria-label={t("approvalCard.confirmFullAccess")}>
                <div><b>{t("approvalCard.fullAccessSkipsFutureTool")}</b><span>{t("approvalCard.denyRulesAndSandboxBoundaries")}</span></div>
                <div>
                  <button className="btn" disabled={switchingFullAccess} onClick={() => setConfirmFullAccess(false)}>{t("common.back")}</button>
                  <button className="btn" data-yolo-confirm="" disabled={switchingFullAccess} onClick={() => void useFullAccess()}>
                    {switchingFullAccess ? t("approvalCard.switching") : t("approvalCard.confirmFullAccess2")}
                  </button>
                </div>
              </div>
            )}
            {sealed && (
              <div className="apv-done">
                {item.verdict === "session" ? (
                  <><b>{t("approvalCard.allowedThisKindWillNot")}</b>{t("approvalCard.theKernelRecordsItFor")}</>
                ) : item.verdict === "always" ? (
                  <><b>{t("approvalCard.allowedRecordedAsARule")}</b>{t("approvalCard.writtenToTheConfigLater")}</>
                ) : item.verdict === "yolo" ? (
                  <><b>{t("approvalCard.fullAccessEnabled")}</b>{t("approvalCard.futureToolsWillNotAsk")}</>
                ) : item.verdict === "deny" ? (
                  <><b>{t("approvalCard.denied")}</b>{t("approvalCard.theAgentTakesTheRefusal")}</>
                ) : item.verdict === "unknown" ? (
                  <><b>{t("approvalCard.handledInAnotherWindow")}</b>{t("approvalCard.referToTheLatestRun")}</>
                ) : (
                  <><b>{t("approvalCard.allowedOnce")}</b>{t("approvalCard.theSameOperationWillAsk")}</>
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
      ? [t("approvalCard.executing"), t("approvalCard.planModeIsOffWhat")]
      : item.verdict === "exit"
        ? [t("approvalCard.notNow2"), t("approvalCard.planModeIsOffThe")]
        : item.verdict === "revise" || item.verdict === "deny"
          ? [t("approvalCard.stillPlanning"), t("approvalCard.sayWhatToChangeBelow")]
          : [t("approvalCard.handledInAnotherWindow"), t("approvalCard.referToTheLatestRun")];
  return (
    <div className="call" data-k="ask" data-prompt={sealed ? "settled" : "pending"}>
      <div className="g">
        <Sym glyph="?" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("approvalCard.thePlanIsYoursTo")}</span>
          <span className="tag">{item.a.tool}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined} aria-busy={!!submitting}>
            <div className="apv-dt">{item.a.reason || t("approvalCard.thePlanIsWrittenWhat")}</div>
            {!sealed && (
              <>
                <div className="apv-ft">
                  <button className="btn" data-primary data-action="decision.plan" data-target={item.a.id} data-value="start"
                    disabled={!!submitting} onClick={() => void decide("start")}>
                    {submitting === "start" ? t("common.submitting") : t("approvalCard.startExecution")}
                  </button>
                  <button className="btn" data-action="decision.plan" data-target={item.a.id} data-value="revise"
                    disabled={!!submitting} onClick={() => void decide("revise")}>
                    {t("approvalCard.reviseThePlan")}
                  </button>
                  <button className="btn" data-action="decision.plan" data-target={item.a.id} data-value="exit"
                    disabled={!!submitting} onClick={() => void decide("exit")}>
                    {t("approvalCard.notNow")}
                  </button>
                </div>
                {/* 这一句就是这张卡存在的理由：过去只有「拒绝」，而它其实是
                    「继续规划」—— 想改计划的人以为自己把计划扔了。 */}
                <div className="apv-note">{t("approvalCard.reviseThePlanStaysIn")}</div>
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
