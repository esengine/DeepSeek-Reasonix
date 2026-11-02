import type { FeedbackCategory } from "../port/feedback";
import type { Shot } from "./feedbackshots";

export interface FeedbackDraft {
  category: FeedbackCategory;
  body: string;
  contact: string;
  shots: Shot[];
}

const EMPTY: FeedbackDraft = { category: "bug", body: "", contact: "", shots: [] };

const CONTACT_KEY = "rx-feedback-contact";
let held: FeedbackDraft | null = null;

export function heldDraft(): FeedbackDraft {
  if (held) return held;
  try {
    return { ...EMPTY, contact: localStorage.getItem(CONTACT_KEY) ?? "" };
  } catch {
    return EMPTY;
  }
}

export function holdDraft(next: FeedbackDraft): void {
  held = next;
}

export function dropDraft(): void {
  held = null;
}

export function rememberContact(contact: string): void {
  try {
    localStorage.setItem(CONTACT_KEY, contact);
  } catch {
    // Unavailable storage must not turn an accepted report into a failed send.
  }
}
