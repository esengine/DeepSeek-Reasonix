import type { TranscriptKernel } from "./transcriptKernel";
import type { QuestionAnchor } from "./transcriptGrouping";

export type QuestionNavigation = {
  question: QuestionAnchor;
  generation: number;
  interaction: number;
  status: "pending" | "locating" | "failed";
  attemptedPage?: unknown;
};

/** An interaction owns permission to navigate, never the source session's data work. */
export class TranscriptNavigation {
  private navigation: QuestionNavigation | null = null;

  constructor(private readonly kernel: TranscriptKernel) {}

  owns(request: QuestionNavigation | null): request is QuestionNavigation {
    return request !== null && request === this.navigation
      && request.generation === this.kernel.generation
      && request.interaction === this.kernel.interactionRevision;
  }

  get current(): QuestionNavigation | null {
    return this.owns(this.navigation) ? this.navigation : null;
  }

  start(question: QuestionAnchor): QuestionNavigation {
    this.kernel.cancelActive("question-replaced");
    return this.navigation = { question, generation: this.kernel.generation,
      interaction: this.kernel.interactionRevision, status: "pending" };
  }

  complete(request: QuestionNavigation): void {
    if (this.owns(request)) this.navigation = null;
  }

  fail(request: QuestionNavigation): void {
    if (this.owns(request)) request.status = "failed";
  }

  locate(request: QuestionNavigation, notify: () => void): void {
    const transaction = this.kernel.activeTransaction;
    if (!transaction || transaction.kind !== "jump") { this.complete(request); return; }
    request.status = "locating";
    this.kernel.onTransactionEnd(transaction, () => {
      if (!this.owns(request)) return;
      if (transaction.status === "expired") this.fail(request);
      else this.complete(request);
      notify();
    });
  }

}
