import { useEffect } from "react";
import { recordFrontendDiagnostic } from "../lib/frontendDiagnosticBridge";

export function useAppDiagnostics(input: {
  activeTabId?: string | null;
  tabCount: number;
  ready: boolean;
  running: boolean;
  hydrating: boolean;
  runtimeTransitioning: boolean;
  contentRevision?: number;
}) {
  useEffect(() => {
    recordFrontendDiagnostic("app", "app.surface", { hasActiveTab: Boolean(input.activeTabId), tabCount: input.tabCount });
  }, [input.activeTabId, input.tabCount]);
  useEffect(() => {
    recordFrontendDiagnostic("app", "app.runtime-state", {
      ready: input.ready, running: input.running, hydrating: input.hydrating,
      runtimeTransitioning: input.runtimeTransitioning, contentRevision: input.contentRevision,
    });
  }, [input.contentRevision, input.hydrating, input.ready, input.running, input.runtimeTransitioning]);
}

export function useSidebarConnectionValidity<T extends { id: string }>(input: {
  connections: readonly T[];
  setConnectionId: (update: (current: string) => string) => void;
}) {
  const { connections, setConnectionId } = input;
  useEffect(() => {
    setConnectionId((current) => !current || connections.some((connection) => connection.id === current) ? current : "");
  }, [connections, setConnectionId]);
}
