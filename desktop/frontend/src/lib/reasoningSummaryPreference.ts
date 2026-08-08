import { useSyncExternalStore } from "react";

const REASONING_SUMMARY_KEY = "reasonix-reasoning-summary";
const REASONING_SUMMARY_EVENT = "reasonix:reasoning-summary";

export function getReasoningSummaryEnabled(): boolean {
  if (typeof localStorage === "undefined") return true;
  return localStorage.getItem(REASONING_SUMMARY_KEY) !== "0";
}

export function setReasoningSummaryEnabled(enabled: boolean): void {
  if (typeof localStorage !== "undefined") {
    localStorage.setItem(REASONING_SUMMARY_KEY, enabled ? "1" : "0");
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent(REASONING_SUMMARY_EVENT, { detail: enabled }));
  }
}

export function onReasoningSummaryPreferenceChange(cb: () => void): () => void {
  const handler = () => cb();
  window.addEventListener(REASONING_SUMMARY_EVENT, handler);
  return () => window.removeEventListener(REASONING_SUMMARY_EVENT, handler);
}

export function useReasoningSummaryEnabled(): boolean {
  return useSyncExternalStore(
    onReasoningSummaryPreferenceChange,
    getReasoningSummaryEnabled,
    getReasoningSummaryEnabled,
  );
}
