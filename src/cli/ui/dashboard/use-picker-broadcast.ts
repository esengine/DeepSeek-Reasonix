import type { MutableRefObject } from "react";
import { useEffect } from "react";
import type {
  DashboardEvent,
  PickerAction,
  PickerItem,
  PickerResolution,
} from "../../../server/context.js";

export interface PickerSnapshot {
  pickerKind: string;
  title: string;
  query?: string;
  items: PickerItem[];
  actions: PickerAction[];
  hasMore?: boolean;
  hint?: string;
}

export interface PickerBroadcastPorts {
  broadcast: (ev: DashboardEvent) => void;
  resolverRef: MutableRefObject<((res: PickerResolution) => void) | null>;
  snapshotRef: MutableRefObject<PickerSnapshot | null>;
}

/** Mirrors a TUI picker into the dashboard via modal-up/down events. Caller passes stable refs from App.tsx so identity does not churn the effect. */
export function usePickerBroadcast(
  active: boolean,
  snapshot: PickerSnapshot,
  onResolve: (res: PickerResolution) => void,
  ports: PickerBroadcastPorts,
): void {
  const { broadcast, resolverRef, snapshotRef } = ports;

  useEffect(() => {
    if (!active) return;
    return () => {
      broadcast({ kind: "modal-down", modalKind: "picker" });
      if (resolverRef.current) resolverRef.current = null;
      if (snapshotRef.current) snapshotRef.current = null;
    };
  }, [active, broadcast, resolverRef, snapshotRef]);

  useEffect(() => {
    if (!active) return;
    snapshotRef.current = snapshot;
    resolverRef.current = onResolve;
    broadcast({ kind: "modal-up", modal: { kind: "picker", ...snapshot } });
  }, [active, snapshot, onResolve, broadcast, resolverRef, snapshotRef]);
}
