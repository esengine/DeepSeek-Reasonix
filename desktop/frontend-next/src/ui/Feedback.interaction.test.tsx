// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Feedback } from "./Feedback";
import { dropDraft } from "./feedbackdraft";
import { install } from "./filedrop";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort } from "../port/port";
import { FEEDBACK_CODE, type FeedbackEnv } from "../port/feedback";

beforeEach(dropDraft);

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  sessionStorage.clear();
});

const png = (name = "a.png", size = 64) => new File([new Uint8Array(size)], name, { type: "image/png" });

function setup(over: Partial<FeedbackEnv> = {}, tab: "send" | "mine" = "send") {
  const port = new MockPort() as unknown as AgentPort;
  const base = port.feedbackEnv.bind(port);
  port.feedbackEnv = vi.fn(async (locale?: string) => ({ ...(await base(locale)), ...over }));
  port.openExternal = vi.fn(async () => {});
  const onClose = vi.fn();
  const onError = vi.fn();
  render(<Feedback port={port} tab={tab} onClose={onClose} onError={onError} />);
  return { port, onClose, onError };
}

const body = () => screen.getByRole<HTMLTextAreaElement>("textbox", { name: /发生了什么/ });
const name = () => screen.getByRole<HTMLInputElement>("textbox", { name: /昵称/ });
const send = () => screen.getByRole<HTMLButtonElement>("button", { name: /发送反馈|重试发送/, hidden: false });
const ready = async () => {
  await screen.findByText("随反馈一起发送的环境信息");
};

async function fill(text = "侧栏在缩放窗口后丢失选中项", nick = "ada") {
  await ready();
  await userEvent.clear(name());
  await userEvent.type(name(), nick);
  await userEvent.type(body(), text);
}

describe("feedback form", () => {
  it("holds Send back until there is text and a nickname, and counts UTF-8 bytes", async () => {
    setup();
    await ready();
    expect(send().disabled).toBe(true);
    await userEvent.type(body(), "你好");
    expect(screen.getByText("6 / 8,192 字节")).toBeTruthy();
    expect(send().disabled).toBe(true);
    await userEvent.type(name(), "ada");
    expect(send().disabled).toBe(false);
  });

  it("refuses text past the byte limit before it is sent", async () => {
    setup({ limits: { bodyBytes: 10, nameChars: 40, contactChars: 120, images: 3, imageBytes: 2 << 20, uploadBytes: 10 << 20, replyBytes: 4096 } });
    await ready();
    await userEvent.type(name(), "ada");
    await userEvent.type(body(), "你你你你");
    expect(body().getAttribute("aria-invalid")).toBe("true");
    expect(send().disabled).toBe(true);
  });

  it("prefills the remembered nickname and shows the environment read-only", async () => {
    setup({ displayName: "老王" });
    await ready();
    expect(name().value).toBe("老王");
    const env = screen.getByText("随反馈一起发送的环境信息").closest("section")!;
    expect(within(env).getByText(/v2\.24\.0/)).toBeTruthy();
    expect(env.querySelector("input, textarea")).toBeNull();
  });

  it("says the report is public and points security reports elsewhere", async () => {
    const { port } = setup();
    await ready();
    expect(screen.getByText("只有被我们登记为议题的反馈才会公开")).toBeTruthy();
    await userEvent.click(screen.getByRole("link", { name: "按安全策略私下报告" }));
    expect(port.openExternal).toHaveBeenCalledWith(expect.stringContaining("/security/policy"));
  });

  it("offers no window-capture control", async () => {
    setup();
    await ready();
    expect(screen.queryByRole("button", { name: /截取|窗口截图|capture/i })).toBeNull();
  });
});

describe("screenshots", () => {
  it("adds from the picker, previews, and removes", async () => {
    setup();
    await ready();
    const input = document.querySelector<HTMLInputElement>('input[type="file"]')!;
    await userEvent.upload(input, [png("one.png"), png("two.png")]);
    expect(await screen.findByAltText("one.png")).toBeTruthy();
    expect(screen.getByAltText("two.png")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "移除截图 one.png" }));
    expect(screen.queryByAltText("one.png")).toBeNull();
    expect(screen.getByAltText("two.png")).toBeTruthy();
  });

  it("takes an image pasted on the form and lets ordinary text paste through", async () => {
    setup();
    await ready();
    const form = document.querySelector("form")!;
    const file = png("clip.png");
    const event = new Event("paste", { bubbles: true, cancelable: true }) as Event & { clipboardData: unknown };
    event.clipboardData = { files: [file], getData: () => "" };
    await act(async () => {
      form.dispatchEvent(event);
    });
    expect(await screen.findByAltText("clip.png")).toBeTruthy();
    expect(event.defaultPrevented).toBe(true);

    const text = new Event("paste", { bubbles: true, cancelable: true }) as Event & { clipboardData: unknown };
    text.clipboardData = { files: [], getData: () => "plain" };
    await act(async () => {
      form.dispatchEvent(text);
    });
    expect(text.defaultPrevented).toBe(false);
  });

  it("takes files dropped on the form", async () => {
    install();
    setup();
    await ready();
    const drop = new Event("drop", { bubbles: true, cancelable: true }) as Event & { dataTransfer: unknown };
    drop.dataTransfer = { types: ["Files"], files: [png("dropped.png")], getData: () => "" };
    await act(async () => {
      document.querySelector("form")!.dispatchEvent(drop);
    });
    expect(await screen.findByAltText("dropped.png", {}, { timeout: 2000 })).toBeTruthy();
  });

  it("names a refused file and adds nothing for it", async () => {
    setup();
    await ready();
    const input = document.querySelector<HTMLInputElement>('input[type="file"]')!;
    fireEvent.change(input, { target: { files: [new File(["x"], "anim.gif", { type: "image/gif" })] } });
    expect(await screen.findByText("anim.gif：只支持 PNG 或 JPEG")).toBeTruthy();
    expect(document.querySelector(".fbk-shots")).toBeNull();
  });

  it("stops at the image limit and past the upload size", async () => {
    setup({ limits: { bodyBytes: 8192, nameChars: 40, contactChars: 120, images: 2, imageBytes: 2 << 20, uploadBytes: 100, replyBytes: 4096 } });
    await ready();
    const input = document.querySelector<HTMLInputElement>('input[type="file"]')!;
    fireEvent.change(input, { target: { files: [png("a.png", 10), png("b.png", 10), png("c.png", 10), png("huge.png", 500)] } });
    expect(await screen.findByText(/huge\.png：超过/)).toBeTruthy();
    expect(screen.getByText(/c\.png：最多 2 张/)).toBeTruthy();
    await waitFor(() => expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(2));
    await waitFor(() => expect(screen.getByRole<HTMLButtonElement>("button", { name: /添加截图/ }).disabled).toBe(true));
  });
});

describe("sending", () => {
  it("sends the whole report once and shows the receipt", async () => {
    const { port } = setup();
    const spy = vi.spyOn(port, "sendFeedback");
    await fill();
    await userEvent.click(screen.getByRole("radio", { name: "建议" }));
    await userEvent.type(screen.getByRole("textbox", { name: /联系方式/ }), "ada@example.com");
    await userEvent.upload(document.querySelector<HTMLInputElement>('input[type="file"]')!, png("s.png"));
    await screen.findByAltText("s.png");
    await userEvent.click(send());

    expect(await screen.findByText("已收到你的反馈")).toBeTruthy();
    const req = spy.mock.calls[0]![0];
    expect(req).toMatchObject({ category: "idea", displayName: "ada", contact: "ada@example.com", body: "侧栏在缩放窗口后丢失选中项" });
    expect(req.idempotencyKey).toMatch(/\S{8,}/);
    expect(req.images).toHaveLength(1);
    expect(req.images[0]).toMatchObject({ name: "s.png" });
    expect(req.images[0]!.dataBase64).not.toMatch(/^data:/);
    expect(screen.getByText(/^FB-[0-9A-Z]{4}-9QX2$/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "复制回执号" })).toBeTruthy();
    expect(screen.queryByText(/已在发出前打码/)).toBeNull();
  });

  it("says when secret-looking text was masked", async () => {
    setup();
    await fill("我的密钥是 sk-abcdefghijklmnop");
    await userEvent.click(send());
    expect(await screen.findByText(/已在发出前打码/)).toBeTruthy();
  });

  it("locks the form while in flight", async () => {
    const { port } = setup();
    let release: (v: unknown) => void = () => {};
    port.sendFeedback = vi.fn(() => new Promise((r) => { release = r; })) as AgentPort["sendFeedback"];
    await fill();
    await userEvent.click(send());
    expect(document.querySelector("form")!.getAttribute("aria-busy")).toBe("true");
    expect(body().matches(":disabled")).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在发送…" }).disabled).toBe(true);
    await act(async () => release({ receipt: "FB-AAAA-BBBB", status: "received", createdAt: "", redacted: false }));
    expect(await screen.findByText("FB-AAAA-BBBB")).toBeTruthy();
  });

  it("starts a fresh report with a new key after success", async () => {
    const { port } = setup();
    const spy = vi.spyOn(port, "sendFeedback");
    await fill();
    await userEvent.click(send());
    await userEvent.click(await screen.findByRole("button", { name: "再写一条" }));
    expect(body().value).toBe("");
    await userEvent.type(body(), "另一件事");
    await userEvent.click(send());
    await screen.findByText("已收到你的反馈");
    expect(spy.mock.calls[1]![0].idempotencyKey).not.toBe(spy.mock.calls[0]![0].idempotencyKey);
  });
});

const CODES: [string, number, Record<string, string | number> | undefined, RegExp][] = [
  [FEEDBACK_CODE.invalid, 400, { field: "body", reason: "too_long" }, /正文太长/],
  [FEEDBACK_CODE.invalid, 400, { field: "images", reason: "format" }, /PNG 或 JPEG/],
  [FEEDBACK_CODE.tooLarge, 413, undefined, /内容太大/],
  [FEEDBACK_CODE.rateLimited, 429, { retryAfterSeconds: 90 }, /90 秒/],
  [FEEDBACK_CODE.rateLimited, 429, undefined, /稍等一会儿/],
  [FEEDBACK_CODE.busy, 503, undefined, /今天的接收量已满/],
  [FEEDBACK_CODE.disabled, 503, undefined, /暂时关闭/],
  [FEEDBACK_CODE.duplicate, 409, undefined, /刚刚已经提交过/],
  [FEEDBACK_CODE.badToken, 409, undefined, /没有认出这台电脑/],
  [FEEDBACK_CODE.offline, 502, undefined, /没能连上反馈服务/],
  [FEEDBACK_CODE.unavailable, 502, undefined, /已填的内容都还在/],
  [FEEDBACK_CODE.internal, 500, undefined, /本机保存反馈记录时出错/],
  [FEEDBACK_CODE.imageMetadata, 400, undefined, /隐藏信息/],
  [FEEDBACK_CODE.badBody, 400, undefined, /没能被解析/],
];

describe("refusals", () => {
  it.each(CODES)("says %s (%#) in its own words and keeps the form", async (code, status, params, words) => {
    const { port } = setup();
    port.sendFeedback = vi.fn(async () => {
      throw new HttpError(status, code, { code, error: "english fallback", params });
    });
    await fill();
    await userEvent.click(send());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(words);
    expect(alert.textContent).not.toMatch(/english fallback/);
    expect(alert.getAttribute("data-code")).toBe(code);
    expect(body().value).toBe("侧栏在缩放窗口后丢失选中项");
    expect(send().disabled).toBe(false);
  });

  it("gives every code a message no other code shares", async () => {
    const seen = new Map<string, string>();
    for (const [code, status, params] of CODES) {
      const { port } = setup();
      port.sendFeedback = vi.fn(async () => {
        throw new HttpError(status, code, { code, error: "x", params });
      });
      await fill();
      await userEvent.click(send());
      const text = (await screen.findByRole("alert")).textContent!;
      const key = `${code}:${JSON.stringify(params ?? {})}`;
      for (const [other, said] of seen) if (said === text) throw new Error(`${key} reads exactly like ${other}`);
      seen.set(key, text);
      cleanup();
    }
  });

  it("does not read the sentence: an unknown code with English prose gets the generic wording", async () => {
    const { port } = setup();
    port.sendFeedback = vi.fn(async () => {
      throw new HttpError(500, "rate limited, too many requests", { code: "feedback.something_new", error: "rate limited" });
    });
    await fill();
    await userEvent.click(send());
    expect((await screen.findByRole("alert")).textContent).toMatch(/反馈没有发出去/);
  });

  it("reuses the same key when the same report is retried after an offline failure", async () => {
    const { port } = setup();
    const spy = vi.spyOn(port, "sendFeedback");
    sessionStorage.setItem("rx-mock-feedback-fault", FEEDBACK_CODE.offline);
    await fill();
    await userEvent.click(send());
    await screen.findByRole("alert");
    sessionStorage.clear();
    await userEvent.click(screen.getByRole("button", { name: "重试发送" }));
    await screen.findByText("已收到你的反馈");
    expect(spy).toHaveBeenCalledTimes(2);
    expect(spy.mock.calls[1]![0].idempotencyKey).toBe(spy.mock.calls[0]![0].idempotencyKey);
  });

  it("takes a new key once the report was edited after a failure", async () => {
    const { port } = setup();
    const spy = vi.spyOn(port, "sendFeedback");
    sessionStorage.setItem("rx-mock-feedback-fault", FEEDBACK_CODE.offline);
    await fill();
    await userEvent.click(send());
    await screen.findByRole("alert");
    sessionStorage.clear();
    await userEvent.type(body(), "，补充一句");
    await userEvent.click(send());
    await screen.findByText("已收到你的反馈");
    expect(spy.mock.calls[1]![0].idempotencyKey).not.toBe(spy.mock.calls[0]![0].idempotencyKey);
  });

  it("treats a request that never reached the kernel like an offline service", async () => {
    const { port } = setup();
    port.sendFeedback = vi.fn(async () => {
      throw new TypeError("Failed to fetch");
    });
    await fill();
    await userEvent.click(send());
    expect((await screen.findByRole("alert")).getAttribute("data-code")).toBe(FEEDBACK_CODE.offline);
    expect(screen.getByRole("button", { name: "重试发送" })).toBeTruthy();
  });

  it("keeps an unavailable report and offers the existing GitHub destination", async () => {
    const { port } = setup();
    sessionStorage.setItem("rx-mock-feedback-fault", FEEDBACK_CODE.unavailable);
    await fill("Neutral report fixture", "tester");
    await userEvent.click(send());
    expect((await screen.findByRole("alert")).textContent).toContain("稍等片刻再试，也可以直接到 GitHub 提交问题");
    expect(body().value).toBe("Neutral report fixture");
    await userEvent.click(screen.getByRole("button", { name: "去 GitHub" }));
    expect(port.openExternal).toHaveBeenCalledWith("https://github.com/esengine/DeepSeek-Reasonix/issues/new/choose");
  });

  it("points a duplicate at My feedback and a disabled channel at GitHub", async () => {
    const { port } = setup();
    port.sendFeedback = vi.fn(async () => {
      throw new HttpError(409, "d", { code: FEEDBACK_CODE.duplicate, error: "d" });
    });
    await fill();
    await userEvent.click(send());
    await userEvent.click(await screen.findByRole("button", { name: "查看我的反馈" }));
    expect(await screen.findByRole("tab", { name: "我的反馈", selected: true })).toBeTruthy();
  });
});

describe("dialog", () => {
  it("closes on Escape and returns focus to what opened it", async () => {
    const opener = document.createElement("button");
    document.body.append(opener);
    opener.focus();
    const { onClose } = setup();
    await ready();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
    cleanup();
    expect(document.activeElement).toBe(opener);
    opener.remove();
  });

  it("moves between the two tabs with the arrow keys", async () => {
    setup();
    await ready();
    await waitFor(() => expect(document.activeElement).toBe(body()));
    const send = screen.getByRole("tab", { name: "发送反馈" });
    send.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "我的反馈", selected: true })).toBeTruthy();
  });

  it("traps Tab and Shift+Tab inside the dialog", async () => {
    const own = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
    Object.defineProperty(HTMLElement.prototype, "offsetParent", { configurable: true, get() { return this.parentNode; } });
    try {
      setup();
      await ready();
      const card = document.querySelector<HTMLElement>('[role="dialog"]')!;
      const seen = new Set<Element>();
      for (let i = 0; i < 60; i++) {
        await userEvent.tab();
        expect(card.contains(document.activeElement)).toBe(true);
        seen.add(document.activeElement!);
      }
      expect(seen.size).toBeGreaterThan(6);
      for (let i = 0; i < 60; i++) {
        await userEvent.tab({ shift: true });
        expect(card.contains(document.activeElement)).toBe(true);
      }
    } finally {
      if (own) Object.defineProperty(HTMLElement.prototype, "offsetParent", own);
      else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
    }
  });

  it("keeps an unsent draft when the dialog is closed and reopened", async () => {
    setup();
    await ready();
    await userEvent.type(body(), "还没写完的话");
    cleanup();
    setup();
    await ready();
    expect(body().value).toBe("还没写完的话");
  });

  it("keeps attached screenshots across close and reopen, and drops them once sent", async () => {
    setup();
    await fill();
    await userEvent.upload(document.querySelector<HTMLInputElement>('input[type="file"]')!, [png("one.png"), png("two.png")]);
    await screen.findByAltText("two.png");
    await userEvent.click(screen.getByRole("button", { name: "移除截图 one.png" }));
    cleanup();

    const again = setup();
    await ready();
    expect(screen.queryByAltText("one.png")).toBeNull();
    const img = screen.getByAltText("two.png") as HTMLImageElement;
    expect(img.src).toMatch(/^data:image\/png/);
    const spy = vi.spyOn(again.port, "sendFeedback");
    await userEvent.type(name(), "ada");
    await userEvent.click(send());
    expect(await screen.findByText("已收到你的反馈")).toBeTruthy();
    expect(spy.mock.calls[0]![0].images).toHaveLength(1);
    expect(spy.mock.calls[0]![0].images[0]).toMatchObject({ name: "two.png" });
    cleanup();

    setup();
    await ready();
    expect(document.querySelector(".fbk-shots")).toBeNull();
    expect(body().value).toBe("");
  });

  it("keeps screenshots when a send fails, and honours the count limit after restore", async () => {
    const { port } = setup({ limits: { bodyBytes: 4000, nameChars: 40, contactChars: 80, images: 2, uploadBytes: 100 } as FeedbackEnv["limits"] });
    port.sendFeedback = vi.fn(async () => { throw new HttpError(500, "boom"); }) as AgentPort["sendFeedback"];
    await fill();
    const input = () => document.querySelector<HTMLInputElement>('input[type="file"]')!;
    await userEvent.upload(input(), [png("a.png", 10), png("b.png", 10)]);
    await waitFor(() => expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(2));
    await userEvent.click(send());
    await screen.findByRole("alert");
    expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(2);
    cleanup();

    setup({ limits: { bodyBytes: 4000, nameChars: 40, contactChars: 80, images: 2, uploadBytes: 100 } as FeedbackEnv["limits"] });
    await ready();
    expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(2);
    fireEvent.change(input(), { target: { files: [png("c.png", 10)] } });
    expect(await screen.findByText(/c\.png：最多 2 张/)).toBeTruthy();
    expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(2);
  });

  it("trims restored screenshots that no longer fit the limits", async () => {
    setup({ limits: { bodyBytes: 4000, nameChars: 40, contactChars: 80, images: 3, uploadBytes: 100 } as FeedbackEnv["limits"] });
    await ready();
    await userEvent.upload(document.querySelector<HTMLInputElement>('input[type="file"]')!, [png("a.png", 10), png("b.png", 10), png("c.png", 10)]);
    await waitFor(() => expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(3));
    cleanup();
    setup({ limits: { bodyBytes: 4000, nameChars: 40, contactChars: 80, images: 2, uploadBytes: 100 } as FeedbackEnv["limits"] });
    await ready();
    await waitFor(() => expect(document.querySelectorAll(".fbk-shots li")).toHaveLength(2));
  });
});
