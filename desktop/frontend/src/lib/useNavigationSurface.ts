import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { flushSync } from "react-dom";
import type { Item } from "./useController";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";
import {
  beginNavigationSurfaceState,
  createNavigationSurfaceTicket,
  markNavigationTargetMasked,
  matchesNavigationSurfaceTicket,
  settleNavigationSurfaceState,
  type NavigationSurfaceTicket,
  type NavigationSurfaceState,
} from "./navigationSurfaceTransition";
import { useCommittedCommand } from "./useCommittedCommand";

export type PreservedTranscriptSurface = {
  tabId?: string;
  items: Item[];
  geometrySessionKey?: string;
};

export function useNavigationSurface(target: {
  activeTabId?: string;
  sessionKey: string;
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

  const begin = useCommittedCommand((nextIntent: number) => {
    recordFrontendDiagnostic("navigation", "navigation.begin", { intent: nextIntent, phase: "begin" });
    const rendered = renderedRef.current;
    flushSync(() => {
      setPreserved(rendered?.items.length ? rendered : null);
      setSurface(beginNavigationSurfaceState(nextIntent));
    });
    renderedRef.current = null;
  });
  const maskTarget = useCommittedCommand((completedIntent: number) => {
    setSurface((current) => markNavigationTargetMasked(current, completedIntent));
  });
  const settle = useCommittedCommand((completedIntent: number, outcome: "ready" | "degraded" | "failed") => {
    if (outcome !== "failed") recordFrontendDiagnostic("navigation", "navigation.paint-ready", { intent: completedIntent, outcome });
    recordFrontendDiagnostic("navigation", "navigation.terminal", { intent: completedIntent, outcome });
    recordFrontendDiagnostic("navigation", "navigation.settle", {
      intent: completedIntent,
      phase: outcome === "failed" ? "data-failed" : "paint-ready",
      outcome,
    });
    setSurface((current) => settleNavigationSurfaceState(current, completedIntent));
    setPreserved(null);
  });
  const ticket = useMemo<NavigationSurfaceTicket | null>(() => {
    if (!dataReady || intent === null || !target.activeTabId) return null;
    return createNavigationSurfaceTicket(intent, target.activeTabId, target.sessionKey);
  }, [dataReady, intent, target.activeTabId, target.sessionKey]);
  const committedTicketRef = useRef<NavigationSurfaceTicket | null>(null);
  useLayoutEffect(() => {
    committedTicketRef.current = ticket;
  }, [ticket]);
  useLayoutEffect(() => () => {
    committedTicketRef.current = null;
    renderedRef.current = null;
  }, []);
  const commitPaint = useCommittedCommand((token: string, outcome: "ready" | "degraded") => {
    const committedTicket = committedTicketRef.current;
    if (!matchesNavigationSurfaceTicket(
      committedTicket,
      token,
      surface?.intent ?? null,
      target.activeTabId,
      target.sessionKey,
    )) return null;
    committedTicketRef.current = null;
    settle(committedTicket!.intent, outcome);
    return committedTicket;
  });
  const commitRendered = useCommittedCommand((rendered: PreservedTranscriptSurface | null) => {
    renderedRef.current = rendered;
  });

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
    surfaceCommitToken: ticket?.token,
    commitRendered,
    begin,
    maskTarget,
    commitPaint,
  };
}
