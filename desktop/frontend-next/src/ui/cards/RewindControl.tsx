import { useCallback, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import type { Checkpoint, RewindPlan, RewindResult, RewindScope } from "../../port/port";
import { useDismiss } from "../dismiss";
import { pinToViewport, useFollow } from "../place";
import { StudioIcon } from "../StudioIcon";

// Gap between the trigger and the menu. It lives here rather than in CSS because
// a portaled menu is placed by measurement, so no rule owns the offset any more.
const GAP = 7;

// Only edit-tool writes are snapshotted. A turn that also ran shell commands
// therefore has gaps the restore cannot reach, and the kernel refuses such a
// plan until it has been shown — so the gaps are what the second stage displays,
// read from the plan rather than written here as a standing caveat.
const SCOPES: { value: RewindScope; label: string; files: boolean }[] = [
  { value: "both", label: "代码和对话", files: true },
  { value: "code", label: "只还原代码", files: true },
  { value: "conversation", label: "只回退对话", files: false },
];

const GAP_REASONS: Record<string, string> = {
  bash_side_effect: "bash 命令所作的修改没有快照",
};

type Stage =
  | { at: "closed" }
  | { at: "menu" }
  | { at: "working" }
  | { at: "confirm"; plan: RewindPlan }
  // The undo is only reachable while this stage holds the transaction id, which
  // is why the menu stays open after a commit instead of closing on success.
  | { at: "done"; tx: string; files: number }
  | { at: "failed"; why: string };

export function RewindControl({
  cp,
  compact = false,
  onPrepare,
  onCommit,
  onUndo,
}: {
  cp: Checkpoint;
  compact?: boolean;
  onPrepare: (turn: number, scope: RewindScope) => Promise<RewindPlan>;
  onCommit: (planId: string) => Promise<RewindResult>;
  onUndo: (transactionId: string) => Promise<void>;
}) {
  const [stage, setStage] = useState<Stage>({ at: "closed" });
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const open = stage.at !== "closed";
  useDismiss(open, wrap, () => setStage({ at: "closed" }), menu);

  // Below the trigger, right edges flush, and above it when the bottom of the
  // window is closer than the menu is tall.
  const place = useCallback(() => {
    const el = menu.current;
    const anchor = btn.current;
    if (!el || !anchor) return;
    const to = anchor.getBoundingClientRect();
    const box = el.getBoundingClientRect();
    const above = to.top - box.height - GAP;
    const fits = to.bottom + GAP + box.height <= innerHeight - 6;
    pinToViewport(el, to.right - box.width, fits || above < 6 ? to.bottom + GAP : above);
  }, []);

  useFollow(open, place, stage);

  const fail = (e: unknown) => setStage({ at: "failed", why: e instanceof Error ? e.message : String(e) });

  const pick = (scope: RewindScope) => {
    setStage({ at: "working" });
    onPrepare(cp.turn, scope)
      .then((plan) => {
        // Nothing to consent to: apply it straight away.
        if (!plan.requiresConfirmation) return onCommit(plan.planId).then((r) => settle(r, plan.fileCount));
        setStage({ at: "confirm", plan });
      })
      .catch(fail);
  };

  const commit = (plan: RewindPlan) => {
    setStage({ at: "working" });
    onCommit(plan.planId)
      .then((r) => settle(r, plan.fileCount))
      .catch(fail);
  };

  // A rewind the kernel can still reverse leaves the offer on screen; one it
  // cannot just closes, because an undo row that fails is worse than none.
  const settle = (result: RewindResult, files: number) => {
    const tx = result.undoAvailable ? (result.transactionId ?? "") : "";
    setStage(tx ? { at: "done", tx, files: result.deleted?.length ?? files } : { at: "closed" });
  };

  const undo = (tx: string) => {
    setStage({ at: "working" });
    onUndo(tx)
      .then(() => setStage({ at: "closed" }))
      .catch(fail);
  };

  return (
    <div className="stepctl rewind" ref={wrap} data-open={open ? "" : undefined}>
      <div className="picker">
        <button
          ref={btn}
          aria-haspopup="menu"
          aria-expanded={open}
          title={t("将工作区与对话回退至该消息之前")}
          aria-label={compact ? t("回到这里") : undefined}
          onClick={() => setStage(stage.at === "closed" ? { at: "menu" } : { at: "closed" })}
        >
          <StudioIcon name="rewind" />{!compact && t("回到这里")}
        </button>
        {open &&
          createPortal(
            // Out of the transcript entirely. The card it sits on carries
            // content-visibility:auto and, from its entrance animation's fill,
            // a transform — either one makes the card both a clip and the
            // containing block for a fixed child, so the menu was cut off at
            // the card's edge and then again at the scroller's. Nothing
            // inside the flow can escape both; a portal does not have to.
            <div className="menu rewindmenu" role="menu" ref={menu}>
              {stage.at === "menu" &&
                SCOPES.filter((s) => cp.files > 0 || !s.files).map((s) => (
                  <button className="mi" role="menuitem" data-action="rewind.prepare" key={s.value} onClick={() => pick(s.value)}>
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t(s.label)}</span>
                    </span>
                    {s.files && <span className="rt">{t("{n} 个文件", { n: cp.files })}</span>}
                  </button>
                ))}
              {stage.at === "menu" && cp.files === 0 && (
                <div className="mi plain">
                  <span className="dot" />
                  <span className="tx">
                    <span className="lb">{t("本轮未修改任何文件")}</span>
                  </span>
                </div>
              )}
              {stage.at === "working" && (
                <div className="mi plain">
                  <span className="dot" />
                  <span className="tx">
                    <span className="lb">{t("正在还原…")}</span>
                  </span>
                </div>
              )}
              {stage.at === "confirm" && (
                <>
                  <div className="mi plain">
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t("本轮有改动无法还原")}</span>
                      <span className="ds">
                        {(stage.plan.coverageGaps ?? [])
                          .map((g) => t(GAP_REASONS[g.reason] ?? g.detail))
                          .join("；") || t("部分改动不在快照内")}
                      </span>
                    </span>
                  </div>
                  <div className="div" />
                  <button className="mi" role="menuitem" data-action="rewind.commit" onClick={() => commit(stage.plan)}>
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t("仍还原其余部分")}</span>
                    </span>
                    <span className="rt">{t("{n} 个文件", { n: stage.plan.fileCount })}</span>
                  </button>
                  <button className="mi plain" role="menuitem" onClick={() => setStage({ at: "closed" })}>
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t("取消")}</span>
                    </span>
                  </button>
                </>
              )}
              {stage.at === "done" && (
                <>
                  <div className="mi plain">
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t("已还原 {n} 个文件", { n: stage.files })}</span>
                    </span>
                  </div>
                  <div className="div" />
                  <button className="mi" role="menuitem" data-action="rewind.undo" onClick={() => undo(stage.tx)}>
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t("撤销这次还原")}</span>
                    </span>
                  </button>
                </>
              )}
              {stage.at === "failed" && (
                <div className="mi plain">
                  <span className="dot" />
                  <span className="tx">
                    <span className="lb">{t("还原失败")}</span>
                    <span className="ds">{stage.why}</span>
                  </span>
                </div>
              )}
            </div>,
            document.body,
          )}
      </div>
    </div>
  );
}
