import { useCallback, useEffect, useRef } from "react";
import { recordFrontendDiagnostic, registerFrontendDiagnosticStartHook } from "./frontendDiagnosticBridge";
import type { ProjectTreeVariant } from "./projectTreeTopic";
import type { ProjectTreeSessionDiagnosticSummary } from "./projectTreeDiagnostics";

export type ProjectTreeDiagnosticSnapshot = ProjectTreeSessionDiagnosticSummary & {
  directoryState: string;
  scope: string;
  variant: ProjectTreeVariant;
  timeFilter: string;
  queryActive: boolean;
  timeFilterActive: boolean;
  catalogPartial: boolean;
  catalogRebuilding: boolean;
  catalogRevision: number;
  catalogIndexed: number;
  catalogTotal: number;
  unloadedSessions: number;
  repairPending: number;
  treeRevision: number;
  organizationRevision: number;
};

function changeReason(previous: ProjectTreeDiagnosticSnapshot | null, current: ProjectTreeDiagnosticSnapshot): string {
  if (!previous) return "initial";
  if (previous.recoveryCopies !== current.recoveryCopies || previous.recoveryCopySessions !== current.recoveryCopySessions) return "recovery-copy";
  if (previous.runtimeOnlySessions !== current.runtimeOnlySessions || previous.runtimeSessions !== current.runtimeSessions) return "runtime-session";
  if (previous.activeSessions !== current.activeSessions || previous.activeVisibleSessions !== current.activeVisibleSessions) return "active-session";
  if (previous.queryActive !== current.queryActive || previous.timeFilterActive !== current.timeFilterActive || previous.hiddenByFilter !== current.hiddenByFilter) return "filter";
  if (previous.visibleSessions !== current.visibleSessions || previous.hiddenSessions !== current.hiddenSessions || previous.hiddenByCollapsed !== current.hiddenByCollapsed || previous.hiddenByTruncation !== current.hiddenByTruncation || previous.expandedFolders !== current.expandedFolders || previous.showAllFolders !== current.showAllFolders) return "visibility";
  if (previous.workspaceSessions !== current.workspaceSessions || previous.folderCount !== current.folderCount || previous.catalogRevision !== current.catalogRevision || previous.catalogIndexed !== current.catalogIndexed || previous.catalogTotal !== current.catalogTotal || previous.unloadedSessions !== current.unloadedSessions || previous.repairPending !== current.repairPending || previous.treeRevision !== current.treeRevision || previous.organizationRevision !== current.organizationRevision || previous.directoryState !== current.directoryState) return "catalog";
  return "directory-update";
}

export function useProjectTreeFrontendDiagnostics(snapshot: ProjectTreeDiagnosticSnapshot): void {
  const currentRef = useRef(snapshot);
  const previousRef = useRef<ProjectTreeDiagnosticSnapshot | null>(null);
  const lastSignatureRef = useRef("");
  currentRef.current = snapshot;
  const signature = JSON.stringify(snapshot);
  const emit = useCallback((force = false, reasonOverride?: string) => {
    const current = currentRef.current;
    if (!force && signature === lastSignatureRef.current) return;
    const previous = previousRef.current;
    recordFrontendDiagnostic("workspace", "workspace.session-directory", {
      ...current,
      changeReason: reasonOverride ?? changeReason(previous, current),
      deltaWorkspaceSessions: previous ? current.workspaceSessions - previous.workspaceSessions : 0,
      deltaVisibleSessions: previous ? current.visibleSessions - previous.visibleSessions : 0,
      deltaHiddenSessions: previous ? current.hiddenSessions - previous.hiddenSessions : 0,
      deltaRecoveryCopies: previous ? current.recoveryCopies - previous.recoveryCopies : 0,
      deltaRuntimeOnlySessions: previous ? current.runtimeOnlySessions - previous.runtimeOnlySessions : 0,
    });
    previousRef.current = current;
    lastSignatureRef.current = signature;
  }, [signature]);
  useEffect(() => emit(), [emit]);
  useEffect(() => registerFrontendDiagnosticStartHook(() => emit(true, "recorder-start")), [emit]);
}
