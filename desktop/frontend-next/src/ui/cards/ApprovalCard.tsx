import type { Item } from "../../state/session";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import type { ApprovalVerdict } from "../../port/port";

// The Tool name the kernel puts on a plan gate; its own comment says frontends
// key their plan UI on it. `kind` says the same thing on newer kernels.
const PLAN_TOOL = "exit_plan_mode";

import type { PlanAction } from "../../port/session";
export type { PlanAction };

interface Props {
  item: Extract<Item, { t: "approval" }>;
  onApprove: (itemId: string, id: string, v: ApprovalVerdict) => void;
  onPlan: (itemId: string, id: string, action: PlanAction) => void;
}

// The run is genuinely blocked here until Approve() resolves it, so this card
// must be the only way past — no other control may advance the queue.
export function ApprovalCard({ item, onApprove, onPlan }: Props) {
  const sealed = item.verdict !== undefined;
  if (item.a.kind === "plan" || item.a.tool === PLAN_TOOL) return <PlanGate item={item} onPlan={onPlan} />;
  return (
    <div className="call" data-k="ask">
      <div className="g">
        <Sym glyph="?" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("要动手了")}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined}>
            <div className="apv-hd">
              <span className="tool">{item.a.tool}</span>
              <span className="sub">{item.a.subject}</span>
            </div>
            {item.a.reason && <div className="apv-dt">{item.a.reason}</div>}
            {!sealed && (
              <div className="apv-ft">
                <button className="btn" data-primary data-action="decision.tool" data-target={item.a.id} data-value="once"
                  onClick={() => onApprove(item.id, item.a.id, "once")}>
                  {t("允许这一次")}
                </button>
                <button className="btn" data-action="decision.tool" data-target={item.a.id} data-value="always"
                  onClick={() => onApprove(item.id, item.a.id, "always")}>
                  {t("这一类不再问")}
                </button>
                <button className="btn" data-action="decision.tool" data-target={item.a.id} data-value="deny"
                  onClick={() => onApprove(item.id, item.a.id, "deny")}>
                  {t("拒绝")}
                </button>
              </div>
            )}
            {sealed && (
              <div className="apv-done">
                {item.verdict === "always" ? (
                  <><b>{t("本会话不再问这一类。")}</b>{t("核心把它记进会话授权，不落盘。")}</>
                ) : item.verdict === "deny" ? (
                  <><b>{t("已拒绝。")}</b>{t("agent 收到否决，会另想办法或停手。")}</>
                ) : item.verdict === "persist" ? (
                  <><b>{t("已记成规则。")}</b>{t("写进了配置，以后的会话也不再问这一类。")}</>
                ) : (
                  <><b>{t("允许这一次。")}</b>{t("下次同样的操作仍会问你。")}</>
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
// 「允许这一次 / 这一类不再问 / 拒绝」. Two of those are wrong for a plan —
// nothing here is repeatable, so there is no class to stop asking about, and the
// kernel refuses a remembered grant for this gate anyway. And the vocabulary hid
// the outcome people actually wanted: denying keeps planning, which is how you
// change the plan. So the card names the three the kernel really distinguishes
// (control.PlanDecisionAction), including the one Studio never offered.
function PlanGate({ item, onPlan }: { item: Props["item"]; onPlan: Props["onPlan"] }) {
  const sealed = item.verdict !== undefined;
  const said =
    item.verdict === "start"
      ? [t("已开始执行。"), t("计划模式已经关掉，接下来是执行。")]
      : item.verdict === "exit"
        ? [t("暂不执行。"), t("已退出计划模式，计划保留在上方，可随时重新交代。")]
        : [t("继续规划。"), t("在下面说要改什么，规划者会据此重写这份计划。")];
  return (
    <div className="call" data-k="ask">
      <div className="g">
        <Sym glyph="?" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("计划等你定")}</span>
          <span className="tag">{item.a.tool}</span>
        </div>
        <div className="out">
          <div className="apv" data-sealed={sealed ? item.verdict : undefined}>
            <div className="apv-dt">{item.a.reason || t("计划已经写好，怎么走由你定。")}</div>
            {!sealed && (
              <>
                <div className="apv-ft">
                  <button className="btn" data-primary data-action="decision.plan" data-target={item.a.id} data-value="start"
                    onClick={() => onPlan(item.id, item.a.id, "start")}>
                    {t("开始执行")}
                  </button>
                  <button className="btn" data-action="decision.plan" data-target={item.a.id} data-value="revise"
                    onClick={() => onPlan(item.id, item.a.id, "revise")}>
                    {t("修改计划")}
                  </button>
                  <button className="btn" data-action="decision.plan" data-target={item.a.id} data-value="exit"
                    onClick={() => onPlan(item.id, item.a.id, "exit")}>
                    {t("暂不执行")}
                  </button>
                </div>
                {/* 这一句就是这张卡存在的理由：过去只有「拒绝」，而它其实是
                    「继续规划」—— 想改计划的人以为自己把计划扔了。 */}
                <div className="apv-note">{t("「修改计划」会留在计划模式里，直接在下面说要改哪里。")}</div>
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
