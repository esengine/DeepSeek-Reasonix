import { useEffect, useState, type RefObject } from "react";
import { t } from "../i18n";
import { reach, type Direction } from "./jumpreach";
import { StudioIcon } from "./StudioIcon";
import { TooltipButton } from "./TooltipButton";

const NONE = { up: false, down: false };

// Two pills riding the transcript's bottom edge. The host has no height of its
// own, so they take nothing from the flow; what they offer is read off the
// scroller, once a frame at most.
export function ScrollJump({ scroll, flow, active, onJump }: {
  scroll: RefObject<HTMLDivElement | null>;
  flow: RefObject<HTMLDivElement | null>;
  active: boolean;
  onJump: (dir: Direction, step: boolean) => void;
}) {
  const [shown, setShown] = useState(NONE);
  useEffect(() => {
    const root = scroll.current;
    if (!root || !active) {
      setShown(NONE);
      return;
    }
    let frame = 0;
    const measure = () => {
      frame = 0;
      const next = reach(root.scrollTop, root.clientHeight, root.scrollHeight);
      setShown((prev) => (prev.up === next.up && prev.down === next.down ? prev : next));
    };
    const ask = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    measure();
    root.addEventListener("scroll", ask, { passive: true });
    const watch = new ResizeObserver(ask);
    watch.observe(root);
    if (flow.current) watch.observe(flow.current);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      root.removeEventListener("scroll", ask);
      watch.disconnect();
    };
  }, [scroll, flow, active]);

  if (!shown.up && !shown.down) return null;
  return (
    <div className="scrolljump">
      <div>
        {shown.up && (
          <TooltipButton data-action="transcript.scroll" data-value="up" aria-label={t("回到会话顶部")} title={t("回到会话顶部（Shift+点击：上一个展开的块）")} onClick={(e) => onJump("up", e.shiftKey)}>
            <StudioIcon name="down" />
          </TooltipButton>
        )}
        {shown.down && (
          <TooltipButton data-action="transcript.scroll" data-value="down" aria-label={t("跳到最新")} title={t("跳到最新（Shift+点击：下一个展开的块）")} onClick={(e) => onJump("down", e.shiftKey)}>
            <StudioIcon name="down" />
          </TooltipButton>
        )}
      </div>
    </div>
  );
}
