import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { reason } from "../../i18n/kernel";
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
  | { at: "failed"; why: string };

export function RewindControl({
  cp,
  compact = false,
  onPrepare,
  onCommit,
}: {
  cp: Checkpoint;
  compact?: boolean;
  onPrepare: (turn: number, scope: RewindScope) => Promise<RewindPlan>;
  onCommit: (planId: string) => Promise<RewindResult>;
}) {
  const [stage, setStage] = useState<Stage>({ at: "closed" });
  const request = useRef({ id: 0, busy: false });
  useEffect(() => () => { request.current.id++; }, []);
  const close = () => { request.current.id++; request.current.busy = false; setStage({ at: "closed" }); };
  const wrap = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const open = stage.at !== "closed";
  useDismiss(open, wrap, close, menu);

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

  const work = async (prepare: () => Promise<RewindPlan>, commitNow: boolean) => {
    if (request.current.busy) return;
    const id = ++request.current.id;
    request.current.busy = true;
    setStage({ at: "working" });
    try {
      const plan = await prepare();
      if (request.current.id !== id) return;
      if (!commitNow && plan.requiresConfirmation) setStage({ at: "confirm", plan });
      else {
        await onCommit(plan.planId);
        if (request.current.id === id) setStage({ at: "closed" });
      }
    } catch (e) {
      if (request.current.id === id) setStage({ at: "failed", why: reason(e) });
    } finally {
      if (request.current.id === id) request.current.busy = false;
    }
  };
  const pick = (scope: RewindScope) => void work(() => onPrepare(cp.turn, scope), false);
  const commit = (plan: RewindPlan) => void work(() => Promise.resolve(plan), true);

  return (
    <div className="stepctl rewind" ref={wrap} data-open={open ? "" : undefined}>
      <div className="picker">
        <button
          ref={btn}
          aria-haspopup="menu"
          aria-expanded={open}
          title={t("将工作区与对话回退至该消息之前")}
          aria-label={compact ? t("回到这里") : undefined}
          onClick={() => stage.at === "closed" ? setStage({ at: "menu" }) : close()}
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
                    <span className="lb">{t("回退范围内未修改任何文件")}</span>
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
                  <button className="mi plain" role="menuitem" onClick={close}>
                    <span className="dot" />
                    <span className="tx">
                      <span className="lb">{t("取消")}</span>
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
