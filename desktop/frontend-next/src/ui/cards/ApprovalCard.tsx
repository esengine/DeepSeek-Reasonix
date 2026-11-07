import type { Item } from "../../state/session";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import type { ApprovalVerdict } from "../../port/port";
import { useState } from "react";
import { decisionSource } from "../source";
import { RememberChoice, RememberWhere, type Remembered } from "./RememberChoice";
import { useViewer } from "../../state/viewer";
import { useFolded } from "../usefold";
import { isPlanGate } from "../planLive";
import { PlanCard } from "./PlanCard";

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
  dynamic_bash: "chat.this_command_runs_another_command",
  browser_credential: "chat.this_step_types_a_password",
  computer_use: "chat.this_reads_or_operates_another",
  computer_front: "chat.this_operates_another_application_on",
  origin_text: "chat.text_in_this_call_is",
  computer_pointer: "chat.this_step_takes_the_pointer",
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
          <span className="apv-dt-label">{t("chat.approving_also")}</span>
          <span>
            {n ? t("chat.starts_n_attempts_in_parallel", { n }) : t("chat.starts_several_attempts_in_parallel")}
            {t("chat.each_running_unattended_in_its")}
          </span>
        </div>
        {task && (
          <div className="apv-dt">
            <span className="apv-dt-label">{t("chat.the_attempts_task")}</span>
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
        <span className="apv-dt-label">{t("chat.approving_also")}</span>
        <span>
          {t("chat.adds_these_n_folders_to", { n: paths.length })}
          <ul className="apv-paths">{paths.map((p) => <li key={p}><code title={p}>{p}</code></li>)}</ul>
        </span>
      </div>
    );
  }
  return (
    <div className="apv-dt" data-scope={scope}>
      <span className="apv-dt-label">{t("chat.approving_also")}</span>
      <span>{t("chat.this_approval_covers_more_than")}</span>
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
  const [moreOpen, setMoreOpen] = useState(false);
  const phone = useFolded("phone");
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
  if (isPlanGate(item.a)) return <PlanCard item={item} onPlan={onPlan} />;
  return (
    // 咨询与授权此前共用 data-k="ask" 和同一个「?」：一个是模型想听你的意见，
    // 另一个是它要动你的文件。授权借「写」的类别 —— 它本来就是一次写权限。
    <div className="call" data-k="write" data-approval={sealed ? item.verdict : "pending"} data-prompt={sealed ? "settled" : "pending"}>
      <div className="g">
        <Sym glyph="⊛" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{item.a.tool === FENCE_TOOL ? t("chat.asking_to_widen_what_it") : isSetting ? t("chat.asks_to_change_a_setting") : t("chat.permission_to_run")}</span>
          <span className="tag" data-pending={sealed ? undefined : ""}>{sealed ? t("chat.handled") : t("chat.needs_you")}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined} aria-busy={!!submitting}>
            <div className="apv-hd" data-whole={item.a.tool === ORIGIN_TOOL ? "" : undefined}>
              <span className="tool">{item.a.tool === FENCE_TOOL ? t("chat.a_file_outside_this_subtasks") : isSetting ? t("common.settings") : item.a.tool === ORIGIN_TOOL ? t("chat.text_from_outside") : item.a.tool}</span>
              <span className="sub" title={item.a.subject}>{item.a.subject}</span>
            </div>
            {item.a.scope && <ApprovalScope scope={item.a.scope} input={item.a.input} />}
            {approvalReason(item.a) && (
              <div className="apv-dt">
                <span className="apv-dt-label">{t("chat.why_confirmation_is_needed")}</span>
                <span style={isSetting ? { whiteSpace: "pre-line" } : undefined}>{approvalReason(item.a)}</span>
              </div>
            )}
            {item.a.tool === SETTING_ELEVATED_TOOL && (
              <div className="apv-dt" data-setting="elevated">
                <span className="apv-dt-label">{t("chat.asked_every_time")}</span>
                <span>{t("chat.this_setting_loosens_what_the")}</span>
              </div>
            )}
            {!sealed && (
              <div className="apv-ft" data-more={moreOpen ? "" : undefined}>
                <button className="btn" data-primary data-action="decision.tool" data-target={item.a.id} data-value="once"
                  disabled={!!submitting} onClick={() => void decide("once")}>
                  {submitting === "once" ? t("common.submitting") : t("chat.allow_once")}
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
                    {t("chat.switch_to_full_access")}
                  </button>
                )}
                {phone && (remembering.length > 0 || explicit) && (
                  <button type="button" className="apv-more" data-action="decision.more" aria-expanded={moreOpen} onClick={() => setMoreOpen((on) => !on)}>
                    {t("chat.more_options")}
                  </button>
                )}
              </div>
            )}
            {!sealed && <RememberWhere options={remembering} target={item.a.id} tool={item.a.tool} />}
            {!sealed && confirmFullAccess && (
              <div className="apv-yolo-confirm" role="alertdialog" aria-label={t("chat.confirm_full_access_2")}>
                <div><b>{t("chat.full_access_skips_future_tool")}</b><span>{t("chat.deny_rules_and_sandbox_boundaries")}</span></div>
                <div>
                  <button className="btn" disabled={switchingFullAccess} onClick={() => setConfirmFullAccess(false)}>{t("common.back")}</button>
                  <button className="btn" data-yolo-confirm="" disabled={switchingFullAccess} onClick={() => void useFullAccess()}>
                    {switchingFullAccess ? t("chat.switching") : t("chat.confirm_full_access")}
                  </button>
                </div>
              </div>
            )}
            {sealed && (
              <div className="apv-done">
                {item.verdict === "session" ? (
                  <><b>{t("chat.allowed_this_kind_will_not")}</b>{t("chat.the_kernel_records_it_for")}</>
                ) : item.verdict === "always" ? (
                  <><b>{t("chat.allowed_recorded_as_a_rule")}</b>{t("chat.written_to_the_config_later")}</>
                ) : item.verdict === "yolo" ? (
                  <><b>{t("chat.full_access_enabled")}</b>{t("chat.future_tools_will_not_ask")}</>
                ) : item.verdict === "deny" ? (
                  <><b>{t("chat.denied")}</b>{t("chat.the_agent_takes_the_refusal")}</>
                ) : item.verdict === "unknown" ? (
                  <><b>{t("chat.handled_in_another_window")}</b>{t("chat.refer_to_the_latest_run")}</>
                ) : (
                  <><b>{t("chat.allowed_once")}</b>{t("chat.the_same_operation_will_ask")}</>
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
