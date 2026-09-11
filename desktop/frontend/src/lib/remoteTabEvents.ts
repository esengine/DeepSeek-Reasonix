import { desktopHost } from "./desktopHost";
import type { RemoteTabState, TabMeta } from "./types";

type MockRemoteTabChannel = "event" | "state";
const mockListeners = new Map<string, Set<(payload: unknown) => void>>();
const openedListeners = new Set<(meta: TabMeta) => void>();
const updatedListeners = new Set<(meta: TabMeta) => void>();

function hostEvents(name: string, cb: (...args: unknown[]) => void): (() => void) | null {
  const host = desktopHost();
  return host.kind === "none" ? null : host.events.on(name, cb);
}

function registerMock(tabId: string, channel: MockRemoteTabChannel, cb: (payload: unknown) => void): () => void {
  const key = `${tabId}:${channel}`;
  const listeners = mockListeners.get(key) ?? new Set<(payload: unknown) => void>();
  listeners.add(cb);
  mockListeners.set(key, listeners);
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0) mockListeners.delete(key);
  };
}

export function onRemoteTabEvent(tabId: string, cb: (frame: unknown) => void): () => void {
  return hostEvents(`remote-tab:${tabId}:event`, cb) ?? registerMock(tabId, "event", cb);
}

export function onRemoteTabState(tabId: string, cb: (state: RemoteTabState) => void): () => void {
  return hostEvents(`remote-tab:${tabId}:state`, (payload?: unknown) => cb((payload ?? {}) as RemoteTabState))
    ?? registerMock(tabId, "state", cb as (payload: unknown) => void);
}

export function onRemoteTabOpened(cb: (meta: TabMeta) => void): () => void {
  const off = hostEvents("remote-tab:opened", (payload?: unknown) => cb((payload ?? {}) as TabMeta));
  if (off) return off;
  openedListeners.add(cb);
  return () => openedListeners.delete(cb);
}

export function onRemoteTabUpdated(cb: (meta: TabMeta) => void): () => void {
  const off = hostEvents("remote-tab:updated", (payload?: unknown) => cb((payload ?? {}) as TabMeta));
  if (off) return off;
  updatedListeners.add(cb);
  return () => updatedListeners.delete(cb);
}

export function __emitMockRemoteTab(tabId: string, channel: MockRemoteTabChannel, payload: unknown): void {
  for (const cb of mockListeners.get(`${tabId}:${channel}`) ?? []) cb(payload);
}

export function __emitMockRemoteTabOpened(meta: TabMeta): void {
  for (const cb of openedListeners) cb(meta);
}

export function __emitMockRemoteTabUpdated(meta: TabMeta): void {
  for (const cb of updatedListeners) cb(meta);
}
