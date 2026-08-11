// remote mirrors the kernel-owned Remote-SSH surfaces: configured hosts,
// per-host connection status, forward snapshots, server-bootstrap state, the
// pending host-key fingerprint, and the dock's host/tab selection. None of it
// is persisted here — the kernel hydrates hosts/statuses on mount and emits
// remote:* updates thereafter.

import { create } from "zustand";

import type {
  RemoteConnectionStatus,
  RemoteFingerprintView,
  RemoteForwardView,
  RemoteHostView,
  RemoteServerView,
  RemoteSecretPromptView,
} from "../lib/types";

export type RemoteExplorerTab = "files" | "ports" | "server";

export type RemoteStatusPopoverRequest = {
  hostId: string;
  nonce: number;
};

export class RemoteConnectionTimeoutError extends Error {
  readonly hostId: string;

  constructor(hostId: string) {
    super(`Timed out connecting to ${hostId}`);
    this.name = "RemoteConnectionTimeoutError";
    this.hostId = hostId;
  }
}

export type RemoteState = {
  hosts: RemoteHostView[];
  statuses: Record<string, RemoteConnectionStatus>;
  forwards: Record<string, RemoteForwardView[]>;
  servers: Record<string, RemoteServerView>;
  pendingFingerprint: RemoteFingerprintView | null;
  pendingSecretPrompt: RemoteSecretPromptView | null;
  statusPopoverRequest: RemoteStatusPopoverRequest | null;
  explorerOpen: boolean;
  explorerHostId: string | null;
  explorerTab: RemoteExplorerTab;

  setHosts: (hosts: RemoteHostView[]) => void;
  applyStatus: (s: RemoteConnectionStatus) => void;
  setStatuses: (list: RemoteConnectionStatus[]) => void;
  hydrateStatuses: (list: RemoteConnectionStatus[]) => void;
  setForwards: (hostId: string, forwards: RemoteForwardView[]) => void;
  setServer: (s: RemoteServerView) => void;
  clearPendingFingerprint: (expected?: RemoteFingerprintView) => void;
  clearPendingSecretPrompt: (expected?: RemoteSecretPromptView) => void;
  requestStatusPopover: (hostId: string) => void;
  clearStatusPopoverRequest: (expected: RemoteStatusPopoverRequest) => void;
  openExplorer: (hostId: string) => void;
  closeExplorer: () => void;
  setExplorerTab: (tab: RemoteExplorerTab) => void;
};

export const useRemoteStore = create<RemoteState>((set) => ({
  hosts: [],
  statuses: {},
  forwards: {},
  servers: {},
  pendingFingerprint: null,
  pendingSecretPrompt: null,
  statusPopoverRequest: null,
  explorerOpen: false,
  explorerHostId: null,
  explorerTab: "files",

  setHosts: (hosts) => set({ hosts }),

  applyStatus: (s) =>
    set((state) => {
      const next: Partial<RemoteState> = {
        statuses: { ...state.statuses, [s.hostId]: s },
      };
      if (s.state === "pending_hostkey" && s.fingerprint) {
        next.pendingFingerprint = s.fingerprint;
      } else if (state.pendingFingerprint?.hostId === s.hostId) {
        // The pending prompt for this host resolved.
        next.pendingFingerprint = null;
      }
      if (s.state === "pending_secret" && s.secretPrompt) {
        next.pendingSecretPrompt = s.secretPrompt;
      } else if (state.pendingSecretPrompt?.hostId === s.hostId) {
        next.pendingSecretPrompt = null;
      }
      return next;
    }),

  setStatuses: (list) =>
    set(() => {
      const statuses: Record<string, RemoteConnectionStatus> = {};
      for (const s of list) statuses[s.hostId] = s;
      return { statuses };
    }),

  hydrateStatuses: (list) =>
    set((state) => {
      const statuses = { ...state.statuses };
      for (const s of list) {
        if (!statuses[s.hostId]) statuses[s.hostId] = s;
      }
      return { statuses };
    }),

  setForwards: (hostId, forwards) =>
    set((state) => ({ forwards: { ...state.forwards, [hostId]: forwards } })),

  setServer: (s) =>
    set((state) => ({ servers: { ...state.servers, [s.hostId]: s } })),

  clearPendingFingerprint: (expected) =>
    set((state) => {
      if (expected && (
        state.pendingFingerprint?.hostId !== expected.hostId ||
        state.pendingFingerprint?.sha256 !== expected.sha256
      )) return state;
      return { pendingFingerprint: null };
    }),

  clearPendingSecretPrompt: (expected) =>
    set((state) => {
      if (expected && (
        state.pendingSecretPrompt?.promptId !== expected.promptId ||
        state.pendingSecretPrompt?.hostId !== expected.hostId ||
        state.pendingSecretPrompt?.kind !== expected.kind ||
        state.pendingSecretPrompt?.identity !== expected.identity
      )) return state;
      return { pendingSecretPrompt: null };
    }),

  requestStatusPopover: (hostId) =>
    set((state) => ({
      statusPopoverRequest: {
        hostId,
        nonce: (state.statusPopoverRequest?.nonce ?? 0) + 1,
      },
    })),

  clearStatusPopoverRequest: (expected) =>
    set((state) => (
      state.statusPopoverRequest?.hostId === expected.hostId &&
      state.statusPopoverRequest.nonce === expected.nonce
        ? { statusPopoverRequest: null }
        : state
    )),

  openExplorer: (hostId) => set({ explorerOpen: true, explorerHostId: hostId }),
  closeExplorer: () => set({ explorerOpen: false }),
  setExplorerTab: (tab) => set({ explorerTab: tab }),
}));

export function waitForRemoteConnection(hostId: string, timeoutMs = 60_000): Promise<void> {
  const connected = (state?: RemoteConnectionStatus["state"]) => state === "connected" || state === "degraded";
  const current = useRemoteStore.getState().statuses[hostId];
  if (connected(current?.state)) return Promise.resolve();
  if (current?.state === "stopped" && current.error) return Promise.reject(new Error(current.error));

  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (err?: Error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      unsubscribe();
      if (err) reject(err);
      else resolve();
    };
    const unsubscribe = useRemoteStore.subscribe((state) => {
      const status = state.statuses[hostId];
      if (connected(status?.state)) finish();
      else if (status?.state === "stopped" && status.error) finish(new Error(status.error));
    });
    const timer = setTimeout(() => finish(new RemoteConnectionTimeoutError(hostId)), timeoutMs);
  });
}
