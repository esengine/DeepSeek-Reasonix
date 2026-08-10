import { useSyncExternalStore } from "react";

export type ReasoningDisplayMode = "hidden" | "summary" | "auto";
export type ResolvedReasoningDisplayMode = ReasoningDisplayMode | "legacy-collapsed" | "pending";

const LEGACY_SUMMARY_KEY = "reasonix-reasoning-summary";
const DISPLAY_EVENT = "reasonix:reasoning-display-mode";

let currentMode: ResolvedReasoningDisplayMode = "auto";
let currentModeExplicit = false;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(DISPLAY_EVENT, { detail: currentMode }));
}

function normalizeMode(value: unknown): ReasoningDisplayMode | undefined {
  return value === "hidden" || value === "summary" || value === "auto" ? value : undefined;
}

function legacySummaryValue(): "on" | "off" | undefined {
  if (typeof localStorage === "undefined") return undefined;
  const stored = localStorage.getItem(LEGACY_SUMMARY_KEY);
  if (stored === "1") return "on";
  if (stored === "0") return "off";
  return undefined;
}

export function resolveReasoningDisplayMode(
  configuredMode: unknown,
  explicit: boolean,
): ResolvedReasoningDisplayMode {
  const normalized = normalizeMode(configuredMode);
  if (explicit && normalized) return normalized;
  switch (legacySummaryValue()) {
    case "off":
      return "legacy-collapsed";
    case "on":
      return "summary";
  }
  return normalized ?? "auto";
}

export function getReasoningDisplayMode(): ResolvedReasoningDisplayMode {
  if (!currentModeExplicit && currentMode !== "pending") {
    const legacy = legacySummaryValue();
    if (legacy === "off") return "legacy-collapsed";
    if (legacy === "on") return "summary";
  }
  return currentMode;
}

export function setReasoningDisplayPending(): void {
  if (currentMode === "pending") return;
  currentMode = "pending";
  currentModeExplicit = false;
  emit();
}

/** Hydrates the frontend mirror from the authoritative Wails startup payload. */
export function hydrateReasoningDisplayMode(configuredMode: unknown, explicit = false): void {
  const next = resolveReasoningDisplayMode(configuredMode, explicit);
  if (next === currentMode && currentModeExplicit === explicit) return;
  currentMode = next;
  currentModeExplicit = explicit;
  emit();
}

/** Applies a successfully persisted user selection and completes legacy migration. */
export function applyReasoningDisplayMode(mode: ReasoningDisplayMode): void {
  if (typeof localStorage !== "undefined") localStorage.removeItem(LEGACY_SUMMARY_KEY);
  if (mode === currentMode && currentModeExplicit) return;
  currentMode = mode;
  currentModeExplicit = true;
  emit();
}

export function onReasoningDisplayModeChange(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

export function useReasoningDisplayMode(): ResolvedReasoningDisplayMode {
  return useSyncExternalStore(onReasoningDisplayModeChange, getReasoningDisplayMode, getReasoningDisplayMode);
}

// Compatibility helpers for older frontend tests/extensions. They keep the
// legacy localStorage key semantics without reintroducing the old settings UI.
export function getReasoningSummaryEnabled(): boolean {
  const mode = getReasoningDisplayMode();
  return mode === "summary" || mode === "auto";
}

export function setReasoningSummaryEnabled(enabled: boolean): void {
  if (typeof localStorage !== "undefined") localStorage.setItem(LEGACY_SUMMARY_KEY, enabled ? "1" : "0");
  if (currentMode !== "summary" && currentMode !== "legacy-collapsed") return;
  currentMode = enabled ? "summary" : "legacy-collapsed";
  currentModeExplicit = false;
  emit();
}

export function useReasoningSummaryEnabled(): boolean {
  const mode = useReasoningDisplayMode();
  return mode === "summary" || mode === "auto";
}
