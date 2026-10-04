import { SseBackup } from "./sse_backup";
import type { FeedbackEnv, FeedbackMine, FeedbackReceipt, FeedbackReplyReceipt, FeedbackRequest } from "./feedback";

export class SseFeedback extends SseBackup {
  feedbackEnv(locale?: string) {
    return this.get<FeedbackEnv>("/feedback/env" + (locale ? "?locale=" + encodeURIComponent(locale) : ""));
  }
  sendFeedback(req: FeedbackRequest) {
    return this.post0<FeedbackReceipt>("/feedback", req);
  }
  myFeedback() {
    return this.get<FeedbackMine>("/feedback/mine");
  }
  replyFeedback(receipt: string, body: string) {
    return this.post0<FeedbackReplyReceipt>("/feedback/" + encodeURIComponent(receipt) + "/reply", { body });
  }
  announceFeedbackReply() {
    return this.post("/notifications/feedback-reply", {});
  }
  feedbackSeen(receipt: string, upTo: number) {
    return this.post("/feedback/" + encodeURIComponent(receipt) + "/seen", { upTo });
  }
}
