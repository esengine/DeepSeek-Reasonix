// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { HttpError } from "../port/http_error";
import { FEEDBACK_CODE } from "../port/feedback";
import { boot, STORAGE } from "../i18n";
import { feedbackFailure, replyFailure } from "./feedbackfailure";

const refuse = (code: string, params?: Record<string, string | number>) =>
  feedbackFailure(new HttpError(400, "whatever the kernel wrote", { code, error: "whatever the kernel wrote", params }));

describe("what a refused feedback asks of the person", () => {
  it("sorts every code into the one next move that helps", () => {
    const retry = (code: string, params?: Record<string, string | number>) => refuse(code, params).retry;
    expect(retry(FEEDBACK_CODE.offline)).toBe("same");
    expect(retry(FEEDBACK_CODE.invalid, { field: "body", reason: "empty" })).toBe("edit");
    expect(retry(FEEDBACK_CODE.tooLarge)).toBe("edit");
    expect(retry(FEEDBACK_CODE.imageMetadata)).toBe("edit");
    expect(retry(FEEDBACK_CODE.rateLimited)).toBe("later");
    expect(retry(FEEDBACK_CODE.busy)).toBe("later");
    expect(retry(FEEDBACK_CODE.unavailable)).toBe("later");
    expect(retry(FEEDBACK_CODE.duplicate)).toBe("none");
    expect(retry(FEEDBACK_CODE.disabled)).toBe("none");
  });

  it("keeps the user's own limit apart from the service's capacity", () => {
    expect(refuse(FEEDBACK_CODE.rateLimited).message).not.toBe(refuse(FEEDBACK_CODE.busy).message);
    expect(refuse(FEEDBACK_CODE.busy).message).not.toMatch(/太频繁/);
  });

  it("says which field an invalid refusal is about", () => {
    const said = new Set(
      [["body", "empty"], ["body", "too_long"], ["displayName", "empty"], ["displayName", "too_long"], ["contact", "too_long"], ["category", "bad_value"], ["images", "too_many"], ["images", "format"], ["images", "too_large"], ["images", "undecodable"]].map(
        ([field, why]) => refuse(FEEDBACK_CODE.invalid, { field: field!, reason: why! }).message,
      ),
    );
    expect(said.size).toBe(10);
  });

  it("never reads the kernel's prose", () => {
    const a = feedbackFailure(new HttpError(429, "too many requests", { code: FEEDBACK_CODE.rateLimited, error: "too many requests" }));
    const b = feedbackFailure(new HttpError(429, "rate limited by the moon", { code: FEEDBACK_CODE.rateLimited, error: "rate limited by the moon" }));
    expect(a).toEqual(b);
    const plain = feedbackFailure(new HttpError(429, "feedback.rate_limited", undefined, false));
    expect(plain.code).not.toBe(FEEDBACK_CODE.rateLimited);
  });

  it("treats anything that is not a kernel answer as the service being unreachable", () => {
    expect(feedbackFailure(new TypeError("Failed to fetch"))).toMatchObject({ code: FEEDBACK_CODE.offline, retry: "same" });
  });
});

describe("what a refused reply asks of the person", () => {
  const reply = (code: string, params?: Record<string, string | number>) =>
    replyFailure(new HttpError(409, "whatever the kernel wrote", { code, error: "whatever the kernel wrote", params }));

  it("sorts every code into the one next move that helps", () => {
    expect(reply(FEEDBACK_CODE.replyLimit).retry).toBe("later");
    expect(reply(FEEDBACK_CODE.notReplyable).retry).toBe("none");
    expect(reply(FEEDBACK_CODE.rateLimited, { retryAfterSeconds: 30 }).retry).toBe("later");
    expect(reply(FEEDBACK_CODE.badToken).retry).toBe("none");
    expect(reply(FEEDBACK_CODE.disabled).retry).toBe("none");
    expect(reply(FEEDBACK_CODE.offline).retry).toBe("same");
    expect(reply(FEEDBACK_CODE.unavailable).retry).toBe("later");
    expect(reply(FEEDBACK_CODE.invalid, { field: "body", reason: "empty" }).retry).toBe("edit");
  });

  it("says each code its own way", () => {
    const said = new Set([
      FEEDBACK_CODE.replyLimit, FEEDBACK_CODE.notReplyable, FEEDBACK_CODE.badToken, FEEDBACK_CODE.disabled, FEEDBACK_CODE.offline, FEEDBACK_CODE.unavailable, FEEDBACK_CODE.internal,
    ].map((code) => reply(code).message));
    expect(said.size).toBe(7);
  });

  it("names the field of an invalid reply and tells an offline one to look before resending", () => {
    expect(reply(FEEDBACK_CODE.invalid, { field: "body", reason: "empty" }).message).not.toBe(reply(FEEDBACK_CODE.invalid, { field: "body", reason: "too_long" }).message);
    expect(reply(FEEDBACK_CODE.offline).message).toMatch(/刷新列表/);
    expect(replyFailure(new TypeError("Failed to fetch"))).toMatchObject({ code: FEEDBACK_CODE.offline, retry: "same" });
  });
});

describe("a verification request on submit", () => {
  it("is something to try later, not something to edit", () => {
    expect(refuse(FEEDBACK_CODE.challengeRequired)).toMatchObject({ code: FEEDBACK_CODE.challengeRequired, retry: "later" });
    expect(refuse(FEEDBACK_CODE.challengeRequired).message).toMatch(/额外验证/);
  });
});

describe("a refusal that names its window", () => {
  const RESET = "2031-03-05T14:30:00Z";
  const clock = (iso: string) => new Date(iso).toLocaleString("zh-CN", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  const submit = (code: string, params: Record<string, string | number | null>, prose = "whatever the kernel wrote") =>
    feedbackFailure(new HttpError(429, prose, { code, error: prose, params: params as Record<string, string | number> }));
  const reply = (code: string, params: Record<string, string | number | null>) =>
    replyFailure(new HttpError(429, "whatever the kernel wrote", { code, error: "whatever the kernel wrote", params: params as Record<string, string | number> }));

  it("says which window was used up and when it resets, from the identifier", () => {
    const said = (limit: string) => submit(FEEDBACK_CODE.rateLimited, { limit, resetsAt: RESET, retryAfterSeconds: 4000 }).message;
    expect(said("install_hourly")).toBe(`这一小时的反馈次数已用完。将在 ${clock(RESET)} 重置。`);
    expect(said("install_daily")).toBe(`今天的反馈次数已用完。将在 ${clock(RESET)} 重置。`);
    expect(said("ip_hourly")).toBe(`这个网络这一小时的提交次数已用完。将在 ${clock(RESET)} 重置。`);
    expect(new Set(["install_hourly", "install_daily", "ip_hourly"].map(said)).size).toBe(3);
    expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_daily", resetsAt: RESET }).retry).toBe("later");
  });

  it("never reads the sentence the service wrote to decide which window it was", () => {
    const a = submit(FEEDBACK_CODE.rateLimited, { limit: "install_daily", resetsAt: RESET }, "hourly limit reached");
    const b = submit(FEEDBACK_CODE.rateLimited, { limit: "install_daily", resetsAt: RESET }, "daily limit reached");
    expect(a).toEqual(b);
    expect(a.message).toContain("今天");
  });

  it("falls back to how long to wait when there is no reset time, and to nothing when there is neither", () => {
    expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_hourly", resetsAt: null, retryAfterSeconds: 90 }).message).toBe("这一小时的反馈次数已用完。约 2 分钟后重置。");
    expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_hourly", resetsAt: "not a time", retryAfterSeconds: 30 }).message).toBe("这一小时的反馈次数已用完。约 30 秒后重置。");
    expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_daily", retryAfterSeconds: 7300 }).message).toBe("今天的反馈次数已用完。约 3 小时后重置。");
    expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_hourly" }).message).toBe("这一小时的反馈次数已用完。");
  });

  it("keeps a limit it has no words for a limit, not a guess", () => {
    expect(submit(FEEDBACK_CODE.rateLimited, { limit: "something_new", resetsAt: RESET }).message).toBe(`已达到一项提交上限。将在 ${clock(RESET)} 重置。`);
  });

  it("still says the plain wait for an older service that sends only the number", () => {
    expect(refuse(FEEDBACK_CODE.rateLimited, { retryAfterSeconds: 90 }).message).toBe("提交得太频繁了，请等 90 秒后再试。");
    expect(refuse(FEEDBACK_CODE.rateLimited).message).toBe("提交得太频繁了，请稍等一会儿再试。");
  });

  it("tells the service's own capacity from the person's limit and names its window", () => {
    const daily = refuse(FEEDBACK_CODE.busy, { limit: "global_daily", resetsAt: RESET });
    const burst = refuse(FEEDBACK_CODE.busy, { limit: "global_burst", retryAfterSeconds: 60 });
    expect(daily.message).toBe(`反馈通道今天的接收量已满，不是你发得太多。内容都还在。将在 ${clock(RESET)} 重置。`);
    expect(burst.message).toBe("反馈通道此刻很忙，不是你发得太多。内容都还在。约 1 分钟后重置。");
    expect(daily.retry).toBe("later");
    expect(refuse(FEEDBACK_CODE.busy).message).toMatch(/请明天再试/);
  });

  it("names a reply window, and a full thread as one that does not reset", () => {
    expect(reply(FEEDBACK_CODE.rateLimited, { limit: "reply_hourly", resetsAt: RESET }).message).toBe(`这一小时的回复次数已用完。将在 ${clock(RESET)} 重置。`);
    const cap = reply(FEEDBACK_CODE.replyLimit, { limit: "reply_item", resetsAt: null, retryAfterSeconds: null });
    expect(cap.message).toBe("这份反馈的回复次数已到上限，不会自动重置。如有新的情况，可以另外提交一条反馈。");
    expect(cap.retry).toBe("none");
    expect(reply(FEEDBACK_CODE.replyLimit, {}).retry).toBe("later");
    expect(reply(FEEDBACK_CODE.replyLimit, {}).message).toMatch(/回复得太频繁/);
  });

  it("speaks English with the same structure", () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    try {
      const en = (iso: string) => new Date(iso).toLocaleString("en", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
      expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_daily", resetsAt: RESET }).message).toBe(`You have used today's feedback allowance. It resets at ${en(RESET)}.`);
      expect(submit(FEEDBACK_CODE.rateLimited, { limit: "install_hourly", retryAfterSeconds: 90 }).message).toBe("You have used this hour's feedback allowance. It resets in about 2 min.");
      expect(reply(FEEDBACK_CODE.replyLimit, { limit: "reply_item" }).message).toBe("This report has reached its reply limit, and it does not reset. If something new comes up, send another report.");
    } finally {
      localStorage.setItem(STORAGE, "zh");
      boot();
    }
  });
});
