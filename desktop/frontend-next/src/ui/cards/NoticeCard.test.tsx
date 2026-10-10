// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
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
  it("names its speaker rather than opening as a bare paragraph", () => {
    const box = draw();
    expect(box.querySelector(".hl .nm")?.textContent).toBe(t("警告"));
    expect(box.querySelector(".hl .tag")?.textContent).toBe(t("主机"));
  });

  it("wears its severity where the gutter can follow it", () => {
    expect(draw().querySelector(".call")?.getAttribute("data-lvl")).toBe("warn");
    expect(draw({ level: "error" }).querySelector(".call")?.getAttribute("data-lvl")).toBe("err");
    expect(draw({ level: "info" }).querySelector(".call")?.getAttribute("data-lvl")).toBeNull();
  });

  // The kernel writes its own English for the log. A code this build has a
  // sentence for is the one the reader gets; only an unknown code falls back.
  it("shows this build's sentence for a code it knows", () => {
    const said = draw().querySelector(".find .t")?.textContent ?? "";
    expect(said).toContain(t("同一个检查连着几轮都报同样的结果，是否继续由你定"));
    expect(said).not.toContain("The same check has failed");
  });

  it("keeps the kernel's diagnostic as the second half", () => {
    const why = draw().querySelector(".find .why")?.textContent ?? "";
    expect(why).toContain("3 rounds");
  });

  it("shows an unknown code's own English rather than nothing", () => {
    const box = draw({ code: "some_future_code", text: "something the kernel said" });
    expect(box.querySelector(".find .t")?.textContent).toContain("something the kernel said");
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
    expect(box.querySelector(".find .why")).toBeNull();
    expect(box.querySelector(".find .t")?.textContent).toContain(t("等待你的输入"));
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
    expect(box.querySelector(".find .t")?.textContent).toBe(t("费用显示币种已设为 {mode}", { mode: "USD" }));
    expect(box.querySelector(".find .why")).toBeNull();
    const auto = draw({ code: "display_currency", text: "kernel", detail: "" });
    expect(auto.querySelector(".find .t")?.textContent).toBe(t("费用显示币种已设为 {mode}", { mode: "auto" }));
  });

  it("words the recovered inbox from its count and draws no raw payload line", () => {
    const box = draw({ level: "warn", code: "inbox_recovered", text: "Recovered 1 pending instruction(s). Inbox is paused — review with /queue before resuming.", detail: '{"count":1}' });
    expect(box.querySelector(".find .t")?.textContent).toBe(t("已恢复 {n} 条未完成的指令。待发送已暂停，请先在输入框上方的队列里查看，再点“继续派发”", { n: 1 }));
    expect(box.querySelector(".find .t")?.textContent).not.toContain("Recovered");
    expect(box.querySelector(".find .why")).toBeNull();
  });

  it("keeps the kernel's English when the recovered-inbox payload is not readable", () => {
    const box = draw({ level: "warn", code: "inbox_recovered", text: "Recovered 2 pending instruction(s).", detail: "garbled" });
    expect(box.querySelector(".find .t")?.textContent).toContain("Recovered 2 pending");
  });

  it("words the dormant permission rules from their payload and draws no raw payload line", () => {
    const detail = '{"rules":[{"list":"ask","rule":"rm","tool":"rm"},{"list":"deny","rule":"git reset","tool":"git reset"}]}';
    const box = draw({ level: "warn", code: "permission_rules_dormant", text: "Permission rules that name no tool match nothing", detail });
    expect(box.querySelector(".find .t")?.textContent).toBe(t("有 {n} 条权限规则没有对应的工具，匹配不到任何调用，因此起不到限制作用（如「{list}」里的 {rule}）；到「设置 → 权限」里删除或改写，命令要写成 Bash(命令:*)", { n: 2, list: t("询问"), rule: "rm" }));
    expect(box.querySelector(".find .why")).toBeNull();
    const garbled = draw({ level: "warn", code: "permission_rules_dormant", text: "Permission rules that name no tool match nothing", detail: "garbled" });
    expect(garbled.querySelector(".find .t")?.textContent).toContain("match nothing");
  });

  it("wraps the user's unapplied guidance in this build's sentence and keeps their words verbatim", () => {
    const box = draw({
      code: "unapplied_steer",
      text: "Guidance was not applied because the turn ended before it could be processed. Send it again if it is still needed:\n同步最新的个人开发管理",
      detail: "同步最新的个人开发管理",
    });
    const said = box.querySelector(".find .t")?.textContent ?? "";
    expect(said).toContain(t("引导没有生效：这一轮在处理它之前就结束了。如果仍然需要，请再发送一次："));
    expect(said).not.toContain("Guidance was not applied");
    expect(box.querySelector(".find .why")?.textContent).toBe("同步最新的个人开发管理");
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
    const said = box.querySelector(".find .t")?.textContent ?? "";
    expect(said).toBe("压缩失败：生成摘要的请求失败了");
    expect(said).not.toContain("upstream");
    expect(box.querySelector(".find .why")).toBeNull();
  });

  it("words a decline from its code, and an empty code as the no-class decline", () => {
    expect(draw({ code: "compact_declined", text: "x", detail: "input_unchanged" }).querySelector(".find .t")?.textContent)
      .toBe("无需压缩：上下文自上次整理后没有变化");
    expect(draw({ code: "compact_declined", text: "x", detail: "" }).querySelector(".find .t")?.textContent)
      .toBe("无需压缩：没有值得折叠的内容");
  });

  it("words a held automatic compaction from the failure code that holds it", () => {
    const box = draw({
      level: "warn",
      code: "compact_held",
      text: "Automatic compaction is paused: the last attempt did not finish (summary_failed).",
      detail: "summary_failed",
    });
    const said = box.querySelector(".find .t")?.textContent ?? "";
    expect(said).toBe("自动压缩暂缓，上次尝试没有完成：生成摘要的请求失败了");
    expect(said).not.toContain("summary_failed");
    expect(box.querySelector(".find .why")).toBeNull();
  });

  it("keeps the kernel's text for a code this build cannot word", () => {
    const box = draw({ code: "compact_failed", text: "compaction failed: kernel english", detail: "future_code" });
    expect(box.querySelector(".find .t")?.textContent).toBe("compaction failed: kernel english");
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

describe("a background job notice", () => {
  const text = (over: Partial<Notice>) => draw({ level: "info", ...over }).querySelector(".find .t")?.textContent ?? "";
  const payload = JSON.stringify({ kind: "bash", id: "bash-126", label: "make build" });

  it("is worded from its typed payload, not the kernel's English", () => {
    const said = text({ code: "job_finished", text: "background bash finished: bash-126", detail: payload });
    expect(said).toBe(t("后台任务已结束：{name}", { name: "make build" }));
    expect(said).not.toContain("background bash");
  });

  it("names the job by id when it carries no label", () => {
    expect(text({ code: "job_killed", text: "x", detail: JSON.stringify({ kind: "task", id: "task-3" }) }))
      .toBe(t("后台任务已终止：{name}", { name: "task-3" }));
  });

  it("names the failed job and keeps its own error underneath", () => {
    const box = draw({ code: "job_failed", text: "background bash failed: bash-1 — boom",
      detail: JSON.stringify({ kind: "bash", id: "bash-1", label: "make build", error: "exit status 2" }) });
    expect(box.querySelector(".find .t")?.textContent).toBe(t("后台任务 {name} 失败，需要处理", { name: "make build" }));
    expect(box.querySelector(".why")?.textContent).toBe("exit status 2");
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
});
