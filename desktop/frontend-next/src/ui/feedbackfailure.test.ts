import { describe, expect, it } from "vitest";
import { HttpError } from "../port/http_error";
import { FEEDBACK_CODE } from "../port/feedback";
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
