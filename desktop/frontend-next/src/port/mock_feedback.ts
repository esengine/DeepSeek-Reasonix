import { HttpError } from "./http_error";
import { MockCommit } from "./mock_commit";
import { FEEDBACK_CODE, type FeedbackEnv, type FeedbackItem, type FeedbackMine, type FeedbackReceipt, type FeedbackReply, type FeedbackReplyReceipt, type FeedbackRequest } from "./feedback";

const LIMITS = { bodyBytes: 8192, nameChars: 40, contactChars: 120, images: 3, imageBytes: 2 << 20, uploadBytes: 10 << 20, replyBytes: 4096 };

const ENV = {
  version: "v2.24.0", commit: "7ee4bbb", surface: "studio", os: "windows", osVersion: "10.0.19045",
  arch: "amd64", locale: "en-US", channel: "stable", providerKind: "deepseek",
};

const DAY = 86_400_000;
const ago = (days: number) => new Date(Date.now() - days * DAY).toISOString();

const item = (over: Partial<FeedbackItem> & Pick<FeedbackItem, "receipt" | "category" | "titleSnippet" | "status" | "createdAt" | "updatedAt">): FeedbackItem => ({
  needsInput: false, replies: [], unreadReplies: 0, ...over,
});

const msg = (id: number, author: FeedbackReply["author"], body: string, days: number): FeedbackReply => ({ id, author, body, createdAt: ago(days) });

// One row per status, so the list is never drawn from a fixture that skips one.
const SEEDED: FeedbackItem[] = [
  item({ receipt: "FB-7K3M-9QX2", category: "bug", titleSnippet: "Sidebar loses its selection after a resize", status: "fixed", issueNumber: 11350, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11350", resolvedVersion: "v2.21.0", createdAt: ago(6), updatedAt: ago(1) }),
  item({ receipt: "FB-2H8P-4WD7", category: "idea", titleSnippet: "Export a session as markdown", status: "recorded", issueNumber: 11302, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11302", createdAt: ago(4), updatedAt: ago(3) }),
  item({ receipt: "FB-5N1C-8RT3", category: "bug", titleSnippet: "Paste of a long log freezes the composer", status: "in_progress", issueNumber: 11377, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11377", createdAt: ago(3), updatedAt: ago(1),
    replies: [msg(21, "maintainer", "Thanks, reproduced. Does it also freeze with a 2 MB log, or only with the 40 MB one?", 2), msg(22, "user", "Only the 40 MB one. The 2 MB log pastes fine.", 1.5)] }),
  item({ receipt: "FB-9B4D-1XM6", category: "question", titleSnippet: "Where do I change the default model?", status: "received", createdAt: ago(0.1), updatedAt: ago(0.1) }),
  item({ receipt: "FB-3F6G-2KV9", category: "idea", titleSnippet: "Fixed in the next build: quieter update banner", status: "fixed", issueNumber: 11390, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11390", resolvedVersion: "next", createdAt: ago(8), updatedAt: ago(0.5) }),
  item({ receipt: "FB-8Q2J-6PW4", category: "other", titleSnippet: "Please support a monorepo layout", status: "wontfix", issueNumber: 11288, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11288", createdAt: ago(12), updatedAt: ago(5) }),
  item({ receipt: "FB-4T7V-3ZH1", category: "bug", titleSnippet: "Crash when opening settings on a small window", status: "duplicate", issueNumber: 11301, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11301", duplicateOf: 11299, createdAt: ago(15), updatedAt: ago(9) }),
  item({ receipt: "FB-6M9R-5CE8", category: "question", titleSnippet: "How do I move my sessions to another disk?", status: "received", statusUnavailable: true, createdAt: ago(40), updatedAt: ago(40) }),
  item({ receipt: "FB-1C5W-7NB2", category: "bug", titleSnippet: "Terminal output is cut off after a resize", status: "needs_info", needsInput: true, createdAt: ago(2), updatedAt: ago(0.2),
    replies: [msg(31, "maintainer", "Thanks for the report. Which operating system and which shell are you using?\nAnd can you say what the window size was before you resized it?", 0.2)] }),
  item({ receipt: "FB-8D3X-4LP6", category: "question", titleSnippet: "Can I use two providers at once?", status: "answered", createdAt: ago(5), updatedAt: ago(4),
    replies: [msg(11, "maintainer", "Yes: add both under Settings > Model services and pick one per session from the model menu.", 4)] }),
  item({ receipt: "FB-2Z6Q-8HK5", category: "other", titleSnippet: "Unrelated advertisement text", status: "closed", createdAt: ago(7), updatedAt: ago(7) }),
];

// A tab's own sessionStorage picks the refusal the next call answers with, so a
// dev page can show every failure without a kernel that will produce it.
const FAULT = "rx-mock-feedback-fault";
const REPLY_FAULT = "rx-mock-feedback-reply-fault";

function fault(key = FAULT): string {
  try {
    return sessionStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

const STATUS: Record<string, number> = {
  [FEEDBACK_CODE.tooLarge]: 413, [FEEDBACK_CODE.rateLimited]: 429, [FEEDBACK_CODE.disabled]: 503,
  [FEEDBACK_CODE.duplicate]: 409, [FEEDBACK_CODE.badToken]: 409, [FEEDBACK_CODE.offline]: 502, [FEEDBACK_CODE.unavailable]: 502, [FEEDBACK_CODE.internal]: 500, [FEEDBACK_CODE.busy]: 503, [FEEDBACK_CODE.imageMetadata]: 400,
  [FEEDBACK_CODE.replyLimit]: 429, [FEEDBACK_CODE.notReplyable]: 409, [FEEDBACK_CODE.challengeRequired]: 403,
};

function refusal(code: string): HttpError {
  return new HttpError(STATUS[code] ?? 500, code, { code, error: code, params: code === FEEDBACK_CODE.rateLimited ? { retryAfterSeconds: 90 } : {} });
}

export class MockFeedback extends MockCommit {
  private filed: FeedbackItem[] = [];
  private name = "";
  private rows: FeedbackItem[] = SEEDED.map((i) => ({ ...i, replies: [...i.replies] }));
  private seen = new Map<string, number>();

  async feedbackEnv(): Promise<FeedbackEnv> {
    return { env: ENV, displayName: this.name, limits: LIMITS };
  }

  async sendFeedback(req: FeedbackRequest): Promise<FeedbackReceipt> {
    const code = fault();
    if (code === "feedback.invalid") {
      throw new HttpError(400, "invalid", { code, error: "invalid", params: { field: "body", reason: "too_long" } });
    }
    if (code && STATUS[code]) throw refusal(code);
    this.name = req.displayName;
    const receipt = "FB-" + (1000 + this.filed.length * 7).toString(36).toUpperCase().padStart(4, "K") + "-9QX2";
    const now = new Date().toISOString();
    this.filed = [item({ receipt, category: req.category, titleSnippet: req.body.trim().slice(0, 80), status: "received", createdAt: now, updatedAt: now }), ...this.filed];
    return { receipt, status: "received", createdAt: now, redacted: /sk-[A-Za-z0-9]{8,}/.test(req.body) };
  }

  async myFeedback(): Promise<FeedbackMine> {
    const code = fault();
    if (code === "mine_error") throw new HttpError(502, "unavailable", { code: FEEDBACK_CODE.unavailable, error: "unavailable" });
    const items = [...this.filed, ...this.rows].map((i) => {
      const unreadReplies = i.replies.filter((r) => r.author === "maintainer" && r.id > (this.seen.get(i.receipt) ?? 0)).length;
      return { ...i, needsInput: i.statusUnavailable ? false : i.needsInput, unreadReplies };
    });
    const unread = items.filter((i) => i.unreadReplies > 0 || i.needsInput).length;
    return { items, offline: code === "mine_offline", unread, hasNew: unread > 0 };
  }

  async replyFeedback(receipt: string, body: string): Promise<FeedbackReplyReceipt> {
    const code = fault(REPLY_FAULT);
    if (code === "feedback.invalid") {
      throw new HttpError(400, "invalid", { code, error: "invalid", params: { field: "body", reason: "empty" } });
    }
    if (code) throw refusal(code);
    const row = this.rows.find((i) => i.receipt === receipt);
    if (!row) throw refusal(FEEDBACK_CODE.notReplyable);
    this.seen.set(receipt, Math.max(0, ...row.replies.map((r) => r.id)));
    const replyId = Math.max(0, ...row.replies.map((r) => r.id)) + 1;
    const createdAt = new Date().toISOString();
    row.replies.push({ id: replyId, author: "user", body, createdAt });
    row.updatedAt = createdAt;
    row.needsInput = false;
    if (row.status === "needs_info") row.status = "received";
    return { replyId, createdAt };
  }

  async feedbackSeen(receipt: string, upTo: number): Promise<void> {
    if (this.rows.some((i) => i.receipt === receipt)) this.seen.set(receipt, Math.max(this.seen.get(receipt) ?? 0, upTo));
  }
}
