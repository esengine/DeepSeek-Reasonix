import { useLayoutEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { currentStep, stepDone, stepLabel, type PlanStep } from "../state/session";

// Keyed by text, not index: todo_write rewrites the whole list every call, and
// under index keys row one morphs into a different sentence instead of the old
// row leaving and a new one arriving. Duplicates get a suffix so React still
// sees one key per row.
// Depth is a number, so it is spent as one rather than as a class per level.
const indent = (s: PlanStep) => (s.level ? Math.min(s.level, 4) * 12 : undefined);

function keys(steps: PlanStep[]): string[] {
  const seen = new Map<string, number>();
  return steps.map((s) => {
    const n = (seen.get(s.text) ?? 0) + 1;
    seen.set(s.text, n);
    return n === 1 ? s.text : `${s.text}#${n}`;
  });
}

export function Plan({ steps, shownElsewhere }: { steps: PlanStep[]; shownElsewhere?: boolean }) {
  const wrap = useRef<HTMLDivElement>(null);
  const [cursor, setCursor] = useState<{ y: number; h: number } | null>(null);
  const now = currentStep(steps);
  const done = steps.filter(stepDone).length;

  // The cursor is measured from the live row, so it survives text wrapping and
  // font changes that a fixed row height would get wrong.
  useLayoutEffect(() => {
    const el = wrap.current?.querySelector<HTMLElement>('.s[data-now]');
    setCursor(el ? { y: el.offsetTop + 6, h: Math.max(0, el.offsetHeight - 11) } : null);
  }, [steps, now]);

  // No plan is not an empty plan: a block reading "0 / 0 · 尚未制定" is a wall
  // to skip on every glance. The task tab is where "there is no plan yet" is
  // an answer worth a line.
  if (steps.length === 0) return null;
  // That same tab draws this list at full width while the rail is still up, so
  // the plan stands twice on one screen in two different hands — numbered with
  // a cursor here, dotted with a label there. The wide one is the reading, and
  // the rail has other things to say with the room.
  if (shownElsewhere) return null;

  const id = keys(steps);
  return (
    <div className="block" data-b="plan">
      <div className="lbl">
        {t("计划")}
        <span className="c">
          {done} / {steps.length}
        </span>
      </div>
      <div className="prog2">
        <span className="track">
          <i style={{ width: `${Math.round((done / steps.length) * 100)}%` }} />
        </span>
      </div>
      <div className="planwrap" ref={wrap}>
        {cursor && <i className="cursor" style={{ height: cursor.h, transform: `translateY(${cursor.y}px)` }} />}
        <div className="plan">
          {steps.map((st, i) => (
            <div
              className="s"
              key={id[i]}
              data-done={stepDone(st) ? "" : undefined}
              data-now={i === now ? "" : undefined}
              style={{ animationDelay: `${Math.min(i, 6) * 34}ms`, marginInlineStart: indent(st) }}
            >
              {/* The state is the shape: a dotted ring is not started, a
                  turning arc is in hand, a filled disc is done. It replaces the
                  ordinal, which was the number the model then cited back. */}
              <span
                className="b"
                aria-label={stepDone(st) ? t("已完成") : i === now ? t("进行中") : t("未开始")}
              />
              {/* The strike is painted as a background so it can be drawn rather
                  than appear, and an inline box is what makes it repeat per line
                  — on the flex item itself a wrapped step gets one line struck. */}
              <span className="t">
                <span className="ln">{stepLabel(st)}</span>
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
