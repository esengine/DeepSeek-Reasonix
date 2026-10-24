// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import "../testkit";
import { NoticeCard } from "./NoticeCard";
import { t } from "../../i18n";
import type { Item } from "../../state/session";

afterEach(cleanup);

type Notice = Extract<Item, { t: "notice" }>;

const notice = (over: Partial<Notice> = {}): Notice => ({
  t: "notice",
  id: "n1",
  level: "warn",
  code: "verification_stalled",
  text: "The same check has failed 3 rounds running and still reports the same thing.",
  detail: "verification stalled: 3 rounds, 5 change(s) landed against it without moving it",
  ...over,
});

const draw = (over: Partial<Notice> = {}) => render(<NoticeCard item={notice(over)} />).container;

// The card is one of the host's, so it is drawn like the others: who is
// speaking, then the body. Both the headline and the gutter are new, and both
// are the difference between a warning and a paragraph a healthy runtime wrote.
describe("a notice card", () => {
  it("shows the holder and claimed path from structured lease data", () => {
    const box = draw({ code: "workspace_lease", detail: undefined,
      workspaceLease: { contended: 0, heldMs: 0, idleMs: 0, holder: "Fixture A", holderSessionId: "session-a", paths: ["src/a.go"], requestedPaths: ["src/a.go"] },
    } as Partial<Notice>);
    expect(box.textContent).toContain("Fixture A");
    expect(box.textContent).toContain("session-a");
    expect(box.textContent).toContain("src/a.go");
  });

  it("shows the granted extent when a wait closes", () => {
    const box = draw({ code: "workspace_lease_resumed", detail: undefined,
      workspaceLease: { contended: 0, heldMs: 0, idleMs: 0, requestedPaths: ["src/b.go"] },
    } as Partial<Notice>);
    expect(box.textContent).toContain("src/b.go");
  });
  it("says its severity to a screen reader and draws it as a glyph, not a header strip", () => {
    const box = draw();
    expect(box.querySelector(".sr-only")?.textContent).toBe(`${t("警告")}：`);
    expect(box.querySelector(".nb-ic")).not.toBeNull();
    expect(box.querySelector(".hl, .tag, .find")).toBeNull();
    expect(draw({ level: "info" }).querySelector(".nb-dot")).not.toBeNull();
  });

  it("wears its severity on the bar so the glyph ink can follow it", () => {
    expect(draw().querySelector(".notice-bar")?.getAttribute("data-lvl")).toBe("warn");
    expect(draw({ level: "error" }).querySelector(".notice-bar")?.getAttribute("data-lvl")).toBe("err");
    expect(draw({ level: "error" }).querySelector(".notice-bar")?.getAttribute("role")).toBe("alert");
    expect(draw({ level: "info" }).querySelector(".notice-bar")?.getAttribute("data-lvl")).toBe("info");
  });

  // The kernel writes its own English for the log. A code this build has a
  // sentence for is the one the reader gets; only an unknown code falls back.
  it("shows this build's sentence for a code it knows", () => {
    const said = draw().querySelector(".notice-bar .t")?.textContent ?? "";
    expect(said).toContain(t("同一个检查连着几轮都报同样的结果，是否继续由你定"));
    expect(said).not.toContain("The same check has failed");
  });

  it("keeps the kernel's diagnostic as the second half", () => {
    const why = draw().querySelector(".notice-bar .nwhy")?.textContent ?? "";
    expect(why).toContain("3 rounds");
  });

  it("shows an unknown code's own English rather than nothing", () => {
    const box = draw({ code: "some_future_code", text: "something the kernel said" });
    expect(box.querySelector(".notice-bar .t")?.textContent).toContain("something the kernel said");
  });
});

// A hand-back's detail is the model's own writing, so it gets the markup a
// reply gets rather than a diagnostic's plain line.
// The renderer is a lazy chunk; its first import under a loaded runner is slow.
const LOADED = { timeout: 10_000 };

describe("a hand-back notice", () => {
  const handBack = {
    level: "info" as const,
    code: "await_user",
    text: "waiting for you: pick a way",
    detail: "**Done so far**\n\n1. **Batch** the rest\n2. Stop here\n\n[docs](https://example.com) <img src=x onerror=alert(1)>",
  };

  it("renders the model's markdown", async () => {
    const box = draw(handBack);
    await waitFor(() => expect(box.querySelector(".nmd strong")).not.toBeNull(), LOADED);
    expect(box.querySelectorAll(".nmd ol li")).toHaveLength(2);
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
    expect(box.querySelector(".notice-bar .t")?.textContent).toContain(t("等待你的输入"));
  });

  it("keeps it to the sanitized subset a reply gets", async () => {
    const box = draw(handBack);
    await waitFor(() => expect(box.querySelector(".nmd a")).not.toBeNull(), LOADED);
    expect(box.querySelector(".nmd a")?.getAttribute("rel")).toContain("noopener");
    expect(box.querySelector(".nmd img[onerror]")).toBeNull();
  });
});

// A notice whose sentence carries figures or the user's own words is drawn from
// the typed payload, so the reader's language owns the whole line.
describe("a coded notice with a payload", () => {
  it("words the currency change from the stored value and draws no second line for it", () => {
    const box = draw({ code: "display_currency", text: "fee display currency set to USD (resolved: USD)", detail: "USD" });
    expect(box.querySelector(".notice-bar .t")?.textContent).toBe(t("费用显示币种已设为 {mode}", { mode: "USD" }));
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
    const auto = draw({ code: "display_currency", text: "kernel", detail: "" });
    expect(auto.querySelector(".notice-bar .t")?.textContent).toBe(t("费用显示币种已设为 {mode}", { mode: "auto" }));
  });

  it("words the recovered inbox from its count and draws no raw payload line", () => {
    const box = draw({ level: "warn", code: "inbox_recovered", text: "Recovered 1 pending instruction(s). Inbox is paused — review with /queue before resuming.", detail: '{"count":1}' });
    expect(box.querySelector(".notice-bar .t")?.textContent).toBe(t("已恢复 {n} 条未完成的指令。待发送已暂停，请先在输入框上方的队列里查看，再点“继续派发”", { n: 1 }));
    expect(box.querySelector(".notice-bar .t")?.textContent).not.toContain("Recovered");
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
  });

  it("keeps the kernel's English when the recovered-inbox payload is not readable", () => {
    const box = draw({ level: "warn", code: "inbox_recovered", text: "Recovered 2 pending instruction(s).", detail: "garbled" });
    expect(box.querySelector(".notice-bar .t")?.textContent).toContain("Recovered 2 pending");
  });

  it("words the dormant permission rules from their payload and draws no raw payload line", () => {
    const detail = '{"rules":[{"list":"ask","rule":"rm","tool":"rm"},{"list":"deny","rule":"git reset","tool":"git reset"}]}';
    const box = draw({ level: "warn", code: "permission_rules_dormant", text: "Permission rules that name no tool match nothing", detail });
    expect(box.querySelector(".notice-bar .t")?.textContent).toBe(t("有 {n} 条权限规则没有对应的工具，匹配不到任何调用，因此起不到限制作用（如「{list}」里的 {rule}）；到「设置 → 权限」里删除或改写，命令要写成 Bash(命令:*)", { n: 2, list: t("询问"), rule: "rm" }));
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
    const garbled = draw({ level: "warn", code: "permission_rules_dormant", text: "Permission rules that name no tool match nothing", detail: "garbled" });
    expect(garbled.querySelector(".notice-bar .t")?.textContent).toContain("match nothing");
  });

  it("wraps the user's unapplied guidance in this build's sentence and keeps their words verbatim", () => {
    const box = draw({
      code: "unapplied_steer",
      text: "Guidance was not applied because the turn ended before it could be processed. Send it again if it is still needed:\n同步最新的个人开发管理",
      detail: "同步最新的个人开发管理",
    });
    const said = box.querySelector(".notice-bar .t")?.textContent ?? "";
    expect(said).toContain(t("引导没有生效：这一轮在处理它之前就结束了。如果仍然需要，请再发送一次："));
    expect(said).not.toContain("Guidance was not applied");
    expect(box.querySelector(".notice-bar .nwhy")?.textContent).toBe("同步最新的个人开发管理");
  });
});

describe("a refused-input notice", () => {
  it("says the message is queued rather than asking to resend it", () => {
    const box = draw({ level: "warn", code: "input_refused", detail: undefined, text: "input was not accepted: the session is being switched — please resend" });
    const said = box.querySelector(".notice-bar .t")?.textContent ?? "";
    expect(said).toBe(t("会话、工作区或运行时正在切换，这条消息没能立刻发出；它已排入队列，切换结束后会自动发送，不需要重发"));
    expect(said).not.toContain("resend");
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
  });
});

describe("a /compact notice", () => {
  it("words a failure from its code in the reader's language and keeps the kernel English out", () => {
    const box = draw({
      level: "warn",
      code: "compact_failed",
      text: "compaction failed: summarizer request failed: upstream said no",
      detail: "summary_failed",
    });
    const said = box.querySelector(".notice-bar .t")?.textContent ?? "";
    expect(said).toBe("压缩失败：生成摘要的请求失败了");
    expect(said).not.toContain("upstream");
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
  });

  it("words a decline from its code, and an empty code as the no-class decline", () => {
    expect(draw({ code: "compact_declined", text: "x", detail: "input_unchanged" }).querySelector(".notice-bar .t")?.textContent)
      .toBe("无需压缩：上下文自上次整理后没有变化");
    expect(draw({ code: "compact_declined", text: "x", detail: "" }).querySelector(".notice-bar .t")?.textContent)
      .toBe("无需压缩：没有值得折叠的内容");
  });

  it("words a held automatic compaction from the failure code that holds it", () => {
    const box = draw({
      level: "warn",
      code: "compact_held",
      text: "Automatic compaction is paused: the last attempt did not finish (summary_failed).",
      detail: "summary_failed",
    });
    const said = box.querySelector(".notice-bar .t")?.textContent ?? "";
    expect(said).toBe("自动压缩暂缓，上次尝试没有完成：生成摘要的请求失败了");
    expect(said).not.toContain("summary_failed");
    expect(box.querySelector(".notice-bar .nwhy")).toBeNull();
  });

  it("keeps the kernel's text for a code this build cannot word", () => {
    const box = draw({ code: "compact_failed", text: "compaction failed: kernel english", detail: "future_code" });
    expect(box.querySelector(".notice-bar .t")?.textContent).toBe("compaction failed: kernel english");
  });

  it("words a skipped extension from its payload and hides the raw payload", () => {
    const box = draw({ code: "extension_skipped", text: "kernel english", detail: JSON.stringify({ extension: "aipush-ask-bridge", point: "tool.before", reason: "no_live_sidecar" }) });
    expect(box.textContent).toContain("扩展 aipush-ask-bridge 的配套后台程序没有运行");
    expect(box.textContent).not.toContain("no_live_sidecar");
  });

  it("keeps the kernel's text for a skipped extension whose payload is unreadable", () => {
    const box = draw({ code: "extension_skipped", text: "kernel english", detail: "not json" });
    expect(box.textContent).toContain("kernel english");
    expect(box.textContent).not.toContain("{ext}");
  });
});

describe("a finished-fold notice", () => {
  const text = (over: Partial<Notice>) => draw({ level: "info", ...over }).querySelector(".notice-bar .t")?.textContent ?? "";
  const payload = JSON.stringify({ mode: "summarized", source_tokens: 122525, projection_tokens: 61192, input_tokens: 40710, output_tokens: 8186, cache_hit_tokens: 256, request_count: 1, user_turns_kept: 157, user_turns_dropped: 6 });

  it("is worded from its typed payload, not as a line of key=value text", () => {
    const said = text({ code: "compaction_telemetry", text: "Context compacted (summarized): about 122525 -> 61192 tokens.", detail: payload });
    expect(said).toContain(t("摘要压缩"));
    expect(said).not.toContain("=");
    expect(said).not.toContain("user_dropped");
    expect(said).not.toContain("Context compacted");
  });

  it("leaves out the usage sentence for a fold that asked for no summary", () => {
    const quiet = JSON.stringify({ mode: "snip", source_tokens: 9000, projection_tokens: 6000, request_count: 0 });
    const said = text({ code: "compaction_telemetry", text: "x", detail: quiet });
    expect(said).toContain(t("裁剪旧工具输出"));
    expect(said).not.toContain(t("摘要请求用了 {in} 输入、{out} 输出 token，缓存命中 {hit}。", { in: "0", out: "0", hit: "0" }));
  });
});

describe("a background job notice", () => {
  const text = (over: Partial<Notice>) => draw({ level: "info", ...over }).querySelector(".notice-bar .t")?.textContent ?? "";
  const payload = JSON.stringify({ kind: "bash", id: "bash-126", label: "make build" });

  it("is worded from its typed payload, not the kernel's English", () => {
    const said = text({ code: "job_finished", text: "background bash finished: bash-126", detail: payload });
    expect(said).toBe(t("后台任务已结束：{name}", { name: "make build" }));
    expect(said).not.toContain("background bash");
  });

  it("words a started job from its payload too, so the start is not the one English line among its siblings", () => {
    const said = text({ code: "job_started", text: "background bash started: bash-126 (make build)", detail: payload });
    expect(said).toBe(t("后台任务已开始：{name}", { name: "make build" }));
    expect(said).not.toContain("background bash");
  });

  it("words a stalled job from its payload: the name, the span, and what it had reported", () => {
    const said = text({ level: "warn", code: "job_stalled", text: "background bash bash-16 has been inactive for 15m0s", detail: JSON.stringify({ kind: "bash", id: "bash-16", label: "make build", quietSeconds: 900, steps: 12 }) });
    expect(said).toBe(t("后台任务 {name} 已有 {quiet} 没有活动（{work}）：可能在等某个东西，也可能卡住了；可以用 wait 或 bash_output 查看，或用 kill_shell 结束它", { name: "make build", quiet: "15m", work: t("{n} 步在静默前完成", { n: 12 }) }));
    expect(said).not.toContain("has been inactive");
  });

  it("says a stalled job reported no progress when it reported no steps", () => {
    const said = text({ level: "warn", code: "job_stalled", text: "x", detail: JSON.stringify({ kind: "bash", id: "bash-16", quietSeconds: 900 }) });
    expect(said).toContain(t("静默前没有任何进展"));
  });

  it("keeps the kernel's text for a stalled job whose payload does not say how long", () => {
    expect(text({ level: "warn", code: "job_stalled", text: "background bash bash-16 has written nothing", detail: JSON.stringify({ kind: "bash", id: "bash-16" }) }))
      .toBe("background bash bash-16 has written nothing");
  });

  it("names the job by id when it carries no label", () => {
    expect(text({ code: "job_killed", text: "x", detail: JSON.stringify({ kind: "task", id: "task-3" }) }))
      .toBe(t("后台任务已终止：{name}", { name: "task-3" }));
  });

  it("names the failed job and keeps its own error underneath", () => {
    const box = draw({ code: "job_failed", text: "background bash failed: bash-1 — boom",
      detail: JSON.stringify({ kind: "bash", id: "bash-1", label: "make build", error: "exit status 2" }) });
    expect(box.querySelector(".notice-bar .t")?.textContent).toBe(t("后台任务 {name} 失败，需要处理", { name: "make build" }));
    expect(box.querySelector(".nwhy")?.textContent).toBe("exit status 2");
  });

  it("keeps the text of a failure stored without a payload", () => {
    expect(text({ code: "job_failed", text: "background bash failed: needs attention", detail: "background bash failed: bash-1 — boom" }))
      .toBe("background bash failed: needs attention");
    expect(text({ code: undefined, text: "background bash failed: needs attention", detail: "x" })).toBe("background bash failed: needs attention");
  });

  it("keeps the text of a stored notice with no code, or an unreadable payload", () => {
    expect(text({ code: undefined, text: "background bash finished: bash-126", detail: undefined })).toBe("background bash finished: bash-126");
    expect(text({ code: "job_finished", text: "background bash finished: bash-1", detail: "not json" })).toBe("background bash finished: bash-1");
  });

  it("words a failed job from its typed exit, not from the raw error text", () => {
    const detail = (extra: object) => JSON.stringify({ kind: "bash", id: "bash-9", label: "make", error: "exit status 2", ...extra });
    const exit = draw({ code: "job_failed", text: "english", detail: detail({ exitCode: 2 }) });
    expect(exit.querySelector(".nwhy")?.textContent).toBe(t("退出码 {code}", { code: 2 }));
    expect(exit.textContent).not.toContain("exit status");
    cleanup();
    const signaled = draw({ code: "job_failed", text: "english", detail: detail({ exitCode: -1, signal: 9 }) });
    expect(signaled.querySelector(".nwhy")?.textContent).toBe(t("被信号 {signal} 终止", { signal: 9 }));
    cleanup();
    const untyped = draw({ code: "job_failed", text: "english", detail: detail({}) });
    expect(untyped.querySelector(".nwhy")?.textContent).toBe("exit status 2");
  });
});

describe("a notice about a setting the model named", () => {
  it.each(["agent.persona.prefix", "agent.persona.suffix", "agent.persona.include_runtime_context"])("opens the user persona editor for %s", (id) => {
    const open = vi.fn();
    const box = render(<NoticeCard item={notice({ level: "info", code: "setting_open", text: "Open the persona editor", detail: id })} onSettings={open} />).container;
    fireEvent.click(box.querySelector("button")!);
    expect(open).toHaveBeenCalledExactlyOnceWith("persona:persona");
  });

  it("offers to open the setting where it is drawn", () => {
    const open = vi.fn();
    const box = render(<NoticeCard item={notice({ level: "info", code: "setting_open", text: "助手需要改动 permissions.mode", detail: "permissions.mode" })} onSettings={open} />).container;
    fireEvent.click(box.querySelector("button")!);
    expect(open).toHaveBeenCalledExactlyOnceWith("tools:approval");
  });

  it("opens the top of the sheet for a setting with no row of its own", () => {
    const open = vi.fn();
    const box = render(<NoticeCard item={notice({ level: "info", code: "setting_open", text: "x", detail: "agent.max_steps" })} onSettings={open} />).container;
    fireEvent.click(box.querySelector("button")!);
    expect(open).toHaveBeenCalledExactlyOnceWith(undefined);
  });

  it("does not draw a button for a change that already happened, and names the setting once", () => {
    const box = draw({ level: "info", code: "setting_changed", text: "设置 peers.enabled 已从 false 改为 true。", detail: "peers.enabled" });
    expect(box.querySelector("button")).toBeNull();
    expect(box.querySelector(".nrule")?.textContent).toBe("peers.enabled");
  });
});
