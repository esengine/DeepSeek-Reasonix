// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, ApprovalMode, Preset, SessionStatus } from "../port/port";

afterEach(cleanup);

const status = (over: Partial<SessionStatus> = {}) =>
  ({ preset: "balanced" as Preset, effort: "auto", toolApprovalMode: "ask" as ApprovalMode,
     plan: false, modelRef: "deepseek/deepseek-v4-pro", ...over } as SessionStatus);

function draw(st: SessionStatus | null = status(), running = false) {
  const r = render(
    <Composer port={new MockPort() as unknown as AgentPort} status={st} running={running}
      focus={0} onSubmit={vi.fn()} onChanged={vi.fn()} onError={vi.fn()} />,
  );
  // The separators are the shelf's own, so counting them says whether anything
  // was left behind by a control that receded.
  const seps = () => r.container.querySelectorAll(".turntools > .sep").length;
  return { ...r, seps };
}

describe("what the composer shows when nothing is unusual", () => {
  // The baseline keeps one quiet door into the per-turn controls without
  // reciting three values the session already had before anyone touched it.
  it("keeps policy discoverable without default-state noise", () => {
    const { container } = draw();
    expect(container.querySelector(".policy[data-quiet]")).toBeTruthy();
    expect(container.querySelector('[data-action="chrome.policy"]')?.getAttribute("aria-label")).toBe("本轮执行策略");
    expect(container.querySelector('[data-action="chrome.policy"]')?.textContent).not.toMatch(/均衡|询问/);
  });

  // A status may recede to its baseline. The only way into a mode may not: the
  // toggle is where plan mode is discovered, and Shift+Tab is a shortcut for
  // people who already know. It stays, saying it is off.
  it("keeps the plan toggle discoverable while plan is off", () => {
    const { container } = draw();
    const tog = container.querySelector('[data-action="plan.mode"]');
    expect(tog).toBeTruthy();
    expect(tog?.getAttribute("aria-pressed")).toBe("false");
    expect(tog?.textContent).toMatch(/计划/);
  });

  // The model has no baseline to fall back to, so every value of it is a real
  // choice and it stays.
  it("keeps the model, which has no default to recede to", async () => {
    draw();
    // A Picker renders its rows into the body, so that is where the choice is.
    await waitFor(() => expect(document.querySelector('[data-action="model.select"]')).toBeTruthy());
  });

  // Model and turn policy are one semantic boundary, regardless of whether a
  // policy value is unusual enough to spell out.
  it("keeps one separator between model and turn controls", () => {
    expect(draw().seps()).toBe(1);
    expect(draw(status({ preset: "delivery" as Preset })).seps()).toBe(1);
  });
});

describe("every deviation is visible", () => {
  it.each([

    ["preset", status({ preset: "delivery" as Preset }), /交付/],
    ["effort", status({ effort: "high" }), /High/],
    ["approval", status({ toolApprovalMode: "auto" as ApprovalMode }), /自动批准/],
    ["a stricter approval", status({ toolApprovalMode: "dontAsk" as ApprovalMode }), /不询问/],
  ])("surfaces %s", (_what, st, want) => {
    const { container } = draw(st);
    expect(container.textContent).toMatch(want);
  });

  // Plan is a control, not a readout, so what deviates is its state.
  it("shows plan mode as engaged once it is on", () => {
    const { container } = draw(status({ plan: true }));
    expect(container.querySelector('[data-action="plan.mode"]')?.getAttribute("aria-pressed")).toBe("true");
  });

  it("surfaces several at once without losing any", () => {
    const { container } = draw(
      status({ preset: "delivery" as Preset, effort: "high", toolApprovalMode: "yolo" as ApprovalMode, plan: true }),
    );
    for (const want of [/交付/, /High/, /全部放行/, /计划/]) expect(container.textContent).toMatch(want);
  });
});

// Sabotage. Collapsing the shelf is allowed to hide a default. It is never
// allowed to hide the one state a person can forget they are in and lose a
// workspace to.
describe("the quiet may not swallow a fully-permitted session", () => {
  it("shows it, marks it, and does so even with everything else at baseline", () => {
    const { container } = draw(status({ toolApprovalMode: "yolo" as ApprovalMode }));
    expect(container.querySelector(".polrisk")?.textContent).toContain("全部放行");
    expect(container.querySelector(".polwarn")?.textContent).toBe("⚠");
  });

  it("shows it while a plan is running too", () => {
    const { container } = draw(status({ toolApprovalMode: "yolo" as ApprovalMode, plan: true }));
    expect(container.textContent).toMatch(/全部放行/);
    expect(container.textContent).toMatch(/计划/);
  });
});

// Which button is filled is the answer to "what would you press now", and while
// a turn runs that is stopping it. Steering is an Enter away and the footer
// says so; stopping has no other way in. Held here rather than only in
// perf/focus.mjs, because that one needs a browser and does not run in CI.
describe("the emphasised action while a turn runs", () => {
  it("is stopping, not steering", () => {
    const { container } = draw(status(), true);
    const primary = container.querySelector(".go .btn[data-primary] span:last-child");
    expect(primary?.textContent).toBe("停下");
  });

  it("leaves steering where sending was, so the rightmost button never moves", () => {
    const idle = draw().container.querySelector(".go .btn:last-child span:last-child")?.textContent;
    cleanup();
    const live = draw(status(), true).container.querySelector(".go .btn:last-child span:last-child")?.textContent;
    expect([idle, live]).toEqual(["发送", "插话"]);
  });

  it("puts it back on sending once the turn is over", () => {
    const { container } = draw(status(), false);
    expect(container.querySelector(".go .btn[data-primary] span:last-child")?.textContent).toBe("发送");
    expect(container.querySelector(".go .btn.stop")).toBeNull();
  });
});
