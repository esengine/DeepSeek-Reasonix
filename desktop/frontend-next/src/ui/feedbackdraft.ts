import type { FeedbackCategory } from "../port/feedback";
import type { Shot } from "./feedbackshots";

export interface FeedbackDraft {
  category: FeedbackCategory;
  body: string;
  contact: string;
  shots: Shot[];
}

const EMPTY: FeedbackDraft = { category: "bug", body: "", contact: "", shots: [] };

let held: FeedbackDraft = EMPTY;

export function heldDraft(): FeedbackDraft {
  return held;
}

export function holdDraft(next: FeedbackDraft): void {
  held = next;
}

export function dropDraft(): void {
  held = EMPTY;
}
