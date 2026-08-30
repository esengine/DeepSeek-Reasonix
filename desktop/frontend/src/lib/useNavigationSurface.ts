import { useCallback, useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import type { Item } from "./useController";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";
import {
  beginNavigationSurfaceState,
  markNavigationTargetMasked,
  settleNavigationSurfaceState,
  type NavigationSurfaceState,
} from "./navigationSurfaceTransition";

export type PreservedTranscriptSurface = {
  tabId?: string;
  items: Item[];
  geometrySessionKey?: string;
};

export function useNavigationSurface(target: {
  activeTabId?: string;
  ready: boolean;
  backendActivationPending: boolean;
  hydrating: boolean;
  hydrateError?: string;
}) {
  const [surface, setSurface] = useState<NavigationSurfaceState>(null);
  const [preserved, setPreserved] = useState<PreservedTranscriptSurface | null>(null);
  const renderedRef = useRef<PreservedTranscriptSurface | null>(null);
  const intent = surface?.intent ?? null;
  const transitioning = intent !== null;
  const dataReady = Boolean(
    surface?.phase === "target-masked" && target.activeTabId && target.ready &&
    !target.backendActivationPending && !target.hydrating && !target.hydrateError,
  );
  const failed = Boolean(
    surface?.phase === "target-masked" && target.activeTabId &&
    !target.backendActivationPending && !target.hydrating && target.hydrateError,
  );

  const begin = useCallback((nextIntent: number) => {
    recordFrontendDiagnostic("navigation", "navigation.begin", { intent: nextIntent, phase: "begin" });
    const rendered = renderedRef.current;
    flushSync(() => {
      setPreserved(rendered?.items.length ? rendered : null);
      setSurface(beginNavigationSurfaceState(nextIntent));
    });
  }, []);
  const maskTarget = useCallback((completedIntent: number) => {
    setSurface((current) => markNavigationTargetMasked(current, completedIntent));
  }, []);
  const settle = useCallback((completedIntent: number, outcome: "ready" | "degraded" | "failed") => {
    if (outcome !== "failed") recordFrontendDiagnostic("navigation", "navigation.paint-ready", { intent: completedIntent, outcome });
    recordFrontendDiagnostic("navigation", "navigation.terminal", { intent: completedIntent, outcome });
    recordFrontendDiagnostic("navigation", "navigation.settle", {
      intent: completedIntent,
      phase: outcome === "failed" ? "data-failed" : "paint-ready",
      outcome,
    });
    setSurface((current) => settleNavigationSurfaceState(current, completedIntent));
  }, []);
  const commitPaint = useCallback((completedIntent: number, outcome: "ready" | "degraded") => {
    settle(completedIntent, outcome);
  }, [settle]);

  const dataReadyIntentRef = useRef<number | null>(null);
  useEffect(() => {
    if (!dataReady || intent === null || dataReadyIntentRef.current === intent) return;
    dataReadyIntentRef.current = intent;
    recordFrontendDiagnostic("navigation", "navigation.target-mounted", { intent });
    recordFrontendDiagnostic("navigation", "navigation.data-ready", { intent, outcome: "ready" });
  }, [dataReady, intent]);
  useEffect(() => {
    if (!failed || intent === null) return;
    recordFrontendDiagnostic("navigation", "navigation.data-ready", { intent, outcome: "failed" });
    settle(intent, "failed");
  }, [failed, intent, settle]);
  useEffect(() => {
    if (surface === null) setPreserved(null);
  }, [surface]);

  return {
    surface,
    intent,
    transitioning,
    dataReady,
    preserved,
    renderedRef,
    begin,
    maskTarget,
    commitPaint,
  };
}
