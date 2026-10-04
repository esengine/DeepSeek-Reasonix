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
  it("words the context-budget notice from its figures, not from the kernel's English", () => {
    const box = draw({
      level: "warn",
      code: "context_budget",
      text: "Context at 83% of the compaction threshold — the model was told it has about 135785 tokens of room left.",
      detail: '{"percent":83,"remaining":135785}',
    });
    const said = box.querySelector(".find .t")?.textContent ?? "";
    expect(said).toBe(t("上下文已用到压缩阈值的 {percent}%，已告知模型约剩 {remaining} 个词元的空间。", { percent: 83, remaining: 135785 }));
    expect(said).not.toContain("compaction threshold");
    expect(box.querySelector(".find .why")).toBeNull();
  });

  it("falls back to the kernel's text when the figures do not decode", () => {
    const box = draw({ code: "context_budget", text: "kernel english", detail: "not json" });
    expect(box.querySelector(".find .t")?.textContent).toContain("kernel english");
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
