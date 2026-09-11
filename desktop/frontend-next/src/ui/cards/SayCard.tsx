import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import { count, decimals } from "../../i18n/format";
import { Sym } from "../Sym";
import type { Item } from "../../state/session";
import { LazyMarkdown } from "../LazyMarkdown";
import { Boundary } from "../Boundary";
import { CopyButton } from "../CopyButton";
import { useRevealed } from "../reveal";

// Folded, the only thing left of a thought is how much of the turn it was. The
// spec puts both halves there — how long, and how much — because either alone
// hides whether a slow turn was spent thinking or waiting.
function thoughtLabel(item: Extract<Item, { t: "say" }>) {
  // Graphemes, not code units: an emoji in the reasoning is one character to
  // the reader and two to the string.
  const chars = t("{n} 字", { n: count([...(item.reasoning ?? "")].length) });
  return item.thoughtMs
    ? t("思考 {secs} 秒 · {chars}", { secs: decimals(item.thoughtMs / 1000, 1), chars })
    : t("思考 {chars}", { chars });
}

export function SayCard({ item }: { item: Extract<Item, { t: "say" }> }) {
  const [open, setOpen] = useState(!item.done);
  const wasDone = useRef(item.done);
  useEffect(() => {
    if (!wasDone.current && item.done) setOpen(false);
    wasDone.current = item.done;
  }, [item.done]);
  // Thinking is the longest-running stream of the turn — 10s of it before the
  // first answer token, measured — so it gets the same paced reveal the answer
  // does rather than tracking the wire's bursts.
  const thought = useRevealed(item.reasoning ?? "", !item.done);
  return (
    <div className="call" data-k="say">
      <div className="g">
        <Sym glyph="◦" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="out">
          {item.reasoning && (
            <details className="think" open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
              <summary>
                <span className="fold">{item.done ? thoughtLabel(item) : t("思考中…")}</span>
              </summary>
              <div className="tk">
                {thought}
                {!item.done && <span className="caret" />}
              </div>
            </details>
          )}
          {item.text && (
            <div className="txt">
              <Boundary fallback={<div className="md">{item.text}</div>}>
                <LazyMarkdown text={item.text} streaming={!item.done} />
              </Boundary>
            </div>
          )}
          {/* Only once the answer is whole: copying half a stream hands over
              something that was never said. */}
          {item.done && item.text.trim() && (
            <div className="acts">
              <CopyButton text={item.text} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
