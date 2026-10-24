// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MeterRail } from "./MeterRail";
import { boot, STORAGE } from "../i18n";
import type { ContextBreakdown, SessionUsage } from "../port/port";
import type { CostQuote } from "../port/wire";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const quote = (over: Partial<CostQuote> = {}): CostQuote => ({ original: { amount: "12.5", currency: "CNY" }, estimated: false, costComplete: true, coverage: "complete", ...over } as CostQuote);

const usage: SessionUsage = {
  requests: 3, promptTokens: 8000, completionTokens: 450, cacheHit: 2300, cacheMiss: 5700, cacheUnknown: 5000, workMs: 72_000,
  sources: [
    { source: "compaction", requests: 1, promptTokens: 5000, completionTokens: 300, cacheHit: 0, cacheMiss: 5000, cacheUnknown: 5000, cost: quote({ coverage: "incomplete", incompleteReason: "no_rate" }) },
    { source: "executor", requests: 2, promptTokens: 3000, completionTokens: 150, cacheHit: 2300, cacheMiss: 700, cost: quote({ original: { amount: "3.2", currency: "CNY" } }) },
  ],
};

const context = { used: 42_000, window: 128_000, system: 3000, tools: 9000, user: 6000, reply: 12000, output: 12000, compact_at: 96_000, boundary: "window", capacity_at: 128_000 } as ContextBreakdown;

function rail(over: { usage?: SessionUsage; quote?: CostQuote; hideAmounts?: boolean; ctx?: ContextBreakdown | null } = {}) {
  return render(
    <MeterRail
      tps={0} trail={[]} running={false} speed={{ output: 0, modelSeconds: 0, average: 0, firstToken: 0 }}
      metrics={{ hit: 0, miss: 0 }} ctx={"ctx" in over ? (over.ctx ?? null) : context} mcp={[]} compact={{ phase: "idle" } as never} cost={[]} wallet={{} as never}
      hideAmounts={over.hideAmounts ?? false} tasks={[]} jobs={[]} onSettings={vi.fn()} onCancelJob={vi.fn(async () => {})}
      usage={"usage" in over ? over.usage : usage} quote={"quote" in over ? over.quote : quote()}
    />,
  );
}

const button = () => screen.getByRole("button", { name: "查看会话概览" });
const card = () => screen.getByRole("dialog", { name: "会话概览" });

describe("the session overview behind the cache meter", () => {
  it("opens from the cache figure and closes on Escape and on a press away", async () => {
    const user = userEvent.setup();
    rail();
    expect(button().getAttribute("aria-expanded")).toBe("false");
    await user.click(button());
    expect(button().getAttribute("aria-expanded")).toBe("true");
    await user.keyboard("{Escape}");
    expect(button().getAttribute("aria-expanded")).toBe("false");
    await user.click(button());
    fireEvent.mouseDown(document.body);
    expect(button().getAttribute("aria-expanded")).toBe("false");
  });

  it("is reachable from the keyboard", async () => {
    const user = userEvent.setup();
    rail();
    button().focus();
    await user.keyboard("{Enter}");
    expect(button().getAttribute("aria-expanded")).toBe("true");
  });

  it("says what each session figure counts, beside the figure", () => {
    rail();
    const view = within(card());
    expect(view.getByText("自本次打开此会话起；恢复或分叉会重新计数")).toBeTruthy();
    expect(view.getByText("请求").closest("div")!.textContent).toContain("3");
    expect(view.getByText("请求").closest("div")!.textContent).toContain("返回了用量与价格的模型响应");
    expect(view.getByText("Token").closest("div")!.textContent).toContain("8.5k");
    expect(view.getByText("工作时长").closest("div")!.textContent).toContain("1 分 12 秒");
    expect(view.getByText("工作时长").closest("div")!.textContent).toContain("含审批与工具等待");
  });

  it("states the hit rate over what the endpoint split, and says what it left out", () => {
    rail();
    const row = within(card()).getByText("缓存命中").closest("div")!;
    expect(row.textContent).toContain("76%");
    expect(row.textContent).toContain("不含服务商未区分的 5k tokens");
  });

  it("floors the rate, so 99.6% of the split does not read as a perfect 100%", () => {
    rail({ usage: { ...usage, cacheHit: 9960, cacheMiss: 40, cacheUnknown: 0 } });
    const row = within(card()).getByText("缓存命中").closest("div")!;
    expect(row.textContent).toContain("99%");
    expect(row.textContent).not.toContain("100%");
    expect(button().textContent).toContain("99%");
  });

  it("lists each source with its own cache and cost, and an unpriced one as unknown", () => {
    rail();
    const rows = [...card().querySelectorAll(".studio-overview-row")].map((r) => r.textContent);
    expect(rows[0]).toContain("压缩");
    expect(rows[0]).toContain("—");
    expect(rows[1]).toContain("主代理");
    expect(rows[1]).toContain("76%");
    expect(rows[1]).toContain("¥3.20");
    const price = card().querySelectorAll(".studio-overview-row")[0]!.querySelector("b[title]");
    expect(price?.getAttribute("title")).toBe("价格未知：no_rate");
  });

  it("never shows a figure as exact for a cost that was only partly priced", () => {
    rail({ quote: quote({ coverage: "partial" }) });
    const row = within(card()).getByText("费用").closest("div")!;
    expect(row.textContent).toContain("≥");
    expect(row.textContent).toContain("部分请求没有价格信息");
  });

  it("gives no amount for an unpriced session, and keeps the reason", () => {
    rail({ quote: quote({ coverage: "incomplete", incompleteReason: "no_rate" }) });
    const row = within(card()).getByText("费用").closest("div")!;
    expect(row.textContent).not.toMatch(/¥|\d\.\d\d/);
    expect(row.textContent).toContain("价格未知：no_rate");
  });

  it("hides the amounts when the person hid them, and says so in no digits", () => {
    rail({ hideAmounts: true });
    expect(card().textContent).not.toContain("¥3.20");
    expect(card().textContent).not.toContain("12.50");
  });

  it("says there is nothing to count yet when no request was billed", () => {
    rail({ usage: { ...usage, requests: 0, sources: [] } });
    expect(within(card()).getByText("这个会话还没有计费的请求")).toBeTruthy();
  });

  it("agrees with the card about the number it shows on the meter", () => {
    rail();
    expect(button().textContent).toContain("76%");
  });

  it("shows the context window in the same card, as the next request's content and not a running total", () => {
    rail();
    const section = within(card()).getByRole("region", { name: "上下文窗口" });
    expect(section.textContent).toContain("42k");
    expect(section.textContent).toContain("128k");
    expect(section.textContent).toContain("33%");
    expect(section.textContent).toContain("下一次请求会带上的内容，是当前值，不是累计");
    expect(section.querySelectorAll("[data-part]").length).toBe(5);
  });

  it("states the automatic fold line only when the kernel has one", () => {
    rail();
    expect(within(card()).getByRole("region", { name: "上下文窗口" }).textContent).toContain("自动压缩于 96k");
    cleanup();
    rail({ ctx: { ...context, compact_at: 0 } });
    expect(within(card()).getByRole("region", { name: "上下文窗口" }).textContent).not.toContain("自动压缩于");
  });

  it("keeps the window section when no request was billed, and omits it when the kernel sent none", () => {
    rail({ usage: { ...usage, requests: 0, sources: [] } });
    expect(within(card()).getByRole("region", { name: "上下文窗口" })).toBeTruthy();
    cleanup();
    rail({ ctx: null });
    expect(within(card()).queryByRole("region", { name: "上下文窗口" })).toBeNull();
  });

  it("hands over to the context details without leaving both cards open", async () => {
    const user = userEvent.setup();
    rail();
    await user.click(button());
    await user.click(within(card()).getByRole("button", { name: "上下文详情" }));
    expect(button().getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByRole("button", { name: "查看上下文与压缩" }).getAttribute("aria-expanded")).toBe("true");
  });
});
