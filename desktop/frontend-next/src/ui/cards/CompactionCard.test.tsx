import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CompactionCard } from "./CompactionCard";
import type { Compaction } from "../../port/wire";
import { FOLD_WHY, NO_CODE_WHY } from "../../i18n/compaction_why";
import { EN as LEGACY } from "../../i18n/en";
import { catalogue } from "../../i18n/msg";

const EN: Record<string, string> = { ...LEGACY, ...catalogue("en") };

const draw = (c: Compaction) => renderToStaticMarkup(<CompactionCard c={c} done />);

// The card said "a threshold was reached" and stopped there. Against a declared
// 1M window that is a fold at 16% with no reason on screen, and the reader
// supplies the only one available: that the host folds whenever it feels like
// it. The host knows which bound fired and how big it is — this is where it says so.
describe("what the card says sent a fold", () => {
  it("names the window's own bound when that one fired instead", () => {
    const html = draw({ trigger: "auto", boundary: "capacity", triggerTokens: 108_800, messages: 40 });
    expect(html).toContain("窗口容量线");
    expect(html).toContain("109k");
  });

  // A replayed session recorded before the boundary rode the event has no
  // boundary to name, and inventing one would put a threshold on screen that
  // nothing measured.
  it("falls back to the old wording rather than guessing a bound", () => {
    const html = draw({ trigger: "auto", messages: 40 });
    expect(html).toContain("上下文达到阈值");
    expect(html).not.toContain("维护线");
  });

  // A fold the user asked for needs no threshold at all: they are the reason.
  it("says nothing about thresholds for a fold the user asked for", () => {
    const html = draw({ trigger: "manual", boundary: "capacity", triggerTokens: 160_000, messages: 40 });
    expect(html).toContain("手动触发");
    expect(html).not.toContain("160k");
  });
});

describe("what the card says when a fold installed nothing", () => {
  it("names the cause the host settled", () => {
    const html = draw({ trigger: "auto", code: "fixed_prefix_above_trigger" });
    expect(html).toContain("压缩未完成");
    expect(html).toContain("无法折叠的固定部分");
    expect(html).not.toContain("压缩完成");
  });

  it("still says only that nothing folded for a code it has no sentence for", () => {
    const html = draw({ trigger: "auto", code: "future_code" });
    expect(html).toContain("本次未折叠任何内容");
    expect(html).not.toContain("future_code");
  });

  it("names a cancelled compaction instead of reading as complete", () => {
    const html = draw({ trigger: "auto", code: "cancelled" });
    expect(html).toContain("压缩未完成");
    expect(html).toContain("压缩被取消了");
    expect(html).not.toContain("压缩完成");
  });

  it("never reads as complete when a fold installed nothing, whatever its code", () => {
    expect(draw({ trigger: "auto", code: "future_code" })).not.toContain("压缩完成");
    expect(draw({ trigger: "auto" })).not.toContain("压缩完成");
  });

  it("has an English sentence for the no-class decline", () => {
    expect(EN[NO_CODE_WHY]).toBeTruthy();
  });

  it.each(Object.entries(FOLD_WHY))("%s has an English sentence", (code, zh) => {
    expect(EN[zh], `no English for ${code}`).toBeTruthy();
    expect(EN[zh]).not.toBe(zh);
  });
});

describe("the live progress line", () => {
  const live = (c: Compaction) => renderToStaticMarkup(<CompactionCard c={c} done={false} />);

  it("is drawn before the first summary token, so the first one does not move the card", () => {
    expect(live({ trigger: "manual", messages: 21 })).toContain("comp-live");
  });

  it("is drawn while the summary streams, on one line", () => {
    expect(live({ trigger: "manual", messages: 21, summary: "## Summary\nfirst\nsecond" })).toContain("comp-live");
  });
});

describe("what a fold leaves behind", () => {
  const folded = { messages: 354, sourceTokens: 607_000, projectionTokens: 316_000 };
  const remains = { total: 316_000, at: 800_000, active: 0, parts: [{ name: "reply", tokens: 150_000 }, { name: "tool_output", tokens: 98_000 }, { name: "user", tokens: 40_000 }, { name: "system", tokens: 0 }], largest: [{ role: "tool", tool: "browser_read", tokens: 60_000 }] };

  it("says what the rest is made of when the fold left a large share", () => {
    const html = renderToStaticMarkup(<CompactionCard c={{ ...folded, remains }} done />);
    expect(html).toContain("折叠后仍有");
    expect(html).toContain("模型回复");
    expect(html).toContain("工具输出");
    expect(html).not.toContain("系统提示");
    expect(html).toContain("browser_read");
  });

  it("says nothing extra when the host sent no composition", () => {
    expect(renderToStaticMarkup(<CompactionCard c={folded} done />)).not.toContain("折叠后仍有");
  });

  it("names the running turn as untouchable", () => {
    const html = renderToStaticMarkup(<CompactionCard c={{ ...folded, remains: { ...remains, active: 12_000 } }} done />);
    expect(html).toContain("折叠动不了它");
  });
});

describe("the lighter card the board draws", () => {
  it("is titled by what it did, tagged auto or manual, and carries no frame of its own", () => {
    const html = draw({ trigger: "auto", boundary: "capacity", triggerTokens: 108_800, messages: 40, sourceTokens: 117_000, projectionTokens: 24_000 });
    expect(html).toContain("较早的内容已归档");
    expect(html).toContain("· 自动");
    expect(html).toContain('data-light=""');
    expect(html).not.toContain("comp-bar");
    expect(draw({ trigger: "manual", messages: 4 })).toContain("· 手动");
  });
});
