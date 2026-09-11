import { useEffect, useRef, useState } from "react";
import type { Tool } from "../port/wire";
import { t } from "../i18n";
import { seconds, tokens } from "../i18n/format";

// What a step cost, in the two units the kernel can state honestly per call:
// wall-clock, and what the call left in the prompt. The spec pairs them in that
// order wherever both are known ("1m04s · 9.5k"), so every card reads the same
// way and the slot never means one thing here and another there.
// 墙钟跨三个数量级 —— 一次读文件 0.2s，一条 bash 一分钟 —— 线性映射会把快的
// 那一批全挤成一个点。对数摊开它们，条只说长短，是什么类别由轨道那条线说。
function tookWidth(ms: number) {
  return 3 + Math.log10(1 + (ms / 1000) * 9) * 22;
}

// A wait that does not say how long it has been waiting reads the same at two
// seconds and at two minutes, which is what "看起来卡住了" is: the only thing
// moving on a running call is a 1.9s pulse on a 14px glyph, and a pulse is not
// a duration. The kernel's startedAt is deliberately not used — it is unix ms
// from the kernel's clock, and over a remote session that is not this window's,
// so counting from it would spend a clock skew as if it were elapsed time. This
// counts on the window's own clock from the moment the window saw the call
// running, and says nothing about a call that was already running when it
// arrived, because that is a duration this window does not know.
function useWatched(running: boolean): number {
  const from = useRef(0);
  const [now, setNow] = useState(0);
  useEffect(() => {
    if (!running) {
      from.current = 0;
      setNow(0);
      return;
    }
    from.current = Date.now();
    setNow(Date.now());
    // One second is the resolution a reader acts on; anything finer is a digit
    // changing for its own sake.
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [running]);
  return from.current && now ? now - from.current : 0;
}

export function Cost({ tools, running = false }: { tools: Tool[]; running?: boolean }) {
  const ms = tools.reduce((n, t) => n + (t.durationMs ?? 0), 0);
  const left = tools.reduce((n, t) => n + (t.contextTokens ?? 0), 0);
  const watched = useWatched(running);
  // Under a second there is nothing to report: a call that answers that fast
  // never looked stuck, and a digit appearing and going is its own noise.
  const live = watched >= 1000 ? watched : 0;
  if (!ms && !left && !live) return null;
  return (
    <span className="cost">
      {ms > 0 && <i className="took" style={{ width: `${tookWidth(ms).toFixed(1)}px` }} aria-hidden="true" />}
      {ms > 0 && <span>{seconds(ms)}</span>}
      {/* No bar while it runs. The bar is a length against a known duration;
          growing one against a duration nobody knows yet is a progress bar that
          cannot be honest about what fraction it has drawn. */}
      {!ms && live > 0 && <span className="live" title={t("这一步已经跑了多久")}>{seconds(live, 0)}</span>}
      {left > 0 && <span title={t("这一步留在上下文里的估算 token")}>{tokens(left)}</span>}
    </span>
  );
}
