// Run: tsx src/__tests__/remote-store.test.ts
//
// Tests for the remote store's status/fingerprint reconciliation and the
// bridge mock's remote:* event fan-out.

import { useRemoteStore, waitForRemoteConnection } from "../store/remote";
import { onRemoteStatus, __emitMockRemote } from "../lib/bridge";
import type { RemoteConnectionStatus } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function reset() {
  useRemoteStore.setState({ hosts: [], statuses: {}, pendingFingerprint: null });
}

useRemoteStore.getState().setHosts([
  { id: "box", label: "box", host: "box.test", port: 22, user: "dev", identityFile: "", proxyJump: "", defaultWorkspace: "/srv/app", serveInstall: "auto", useSSHConfig: false },
]);
eq(useRemoteStore.getState().hosts[0]?.defaultWorkspace, "/srv/app", "configured hosts hydrate persistent UI state");

// pending_hostkey status sets the pending fingerprint that drives the dialog.
reset();
useRemoteStore.getState().applyStatus({
  hostId: "box",
  state: "pending_hostkey",
  fingerprint: { hostId: "box", address: "1.2.3.4:22", keyType: "ssh-ed25519", sha256: "AAAA" },
});
eq(useRemoteStore.getState().pendingFingerprint?.sha256, "AAAA", "pending_hostkey sets fingerprint");

// A stale dialog completion must not clear a newer fingerprint.
const oldFingerprint = useRemoteStore.getState().pendingFingerprint!;
useRemoteStore.getState().applyStatus({
  hostId: "other",
  state: "pending_hostkey",
  fingerprint: { hostId: "other", address: "2.3.4.5:22", keyType: "ssh-ed25519", sha256: "BBBB" },
});
useRemoteStore.getState().clearPendingFingerprint(oldFingerprint);
eq(useRemoteStore.getState().pendingFingerprint?.sha256, "BBBB", "stale dialog cannot clear newer fingerprint");

// A subsequent non-pending status for the same host clears the fingerprint.
useRemoteStore.getState().applyStatus({ hostId: "other", state: "connected" });
eq(useRemoteStore.getState().pendingFingerprint, null, "resolution clears fingerprint");
eq(useRemoteStore.getState().statuses["other"]?.state, "connected", "status recorded");

// setStatuses replaces the whole map (mount hydration).
useRemoteStore.getState().setStatuses([
  { hostId: "a", state: "connected" },
  { hostId: "b", state: "reconnecting", attempt: 2 },
]);
eq(Object.keys(useRemoteStore.getState().statuses).sort(), ["a", "b"], "setStatuses hydrates");
eq(useRemoteStore.getState().statuses["b"]?.attempt, 2, "attempt preserved");

// Late hydration fills missing hosts without overwriting a newer live event.
useRemoteStore.getState().applyStatus({ hostId: "live", state: "connected" });
useRemoteStore.getState().hydrateStatuses([
  { hostId: "live", state: "connecting" },
  { hostId: "snapshot-only", state: "connected" },
]);
eq(useRemoteStore.getState().statuses["live"]?.state, "connected", "hydration preserves newer live status");
eq(useRemoteStore.getState().statuses["snapshot-only"]?.state, "connected", "hydration fills missing status");

const connectionReady = waitForRemoteConnection("waiting", 1_000);
useRemoteStore.getState().applyStatus({ hostId: "waiting", state: "connected" });
await connectionReady;
eq(useRemoteStore.getState().statuses["waiting"]?.state, "connected", "connection waiter resolves on live connected status");

useRemoteStore.getState().applyStatus({ hostId: "failed", state: "stopped", error: "handshake failed" });
let failedConnection = "";
try {
  await waitForRemoteConnection("failed", 1_000);
} catch (err) {
  failedConnection = err instanceof Error ? err.message : String(err);
}
eq(failedConnection, "handshake failed", "connection waiter rejects the host error without waiting for timeout");

// The bridge mock fan-out delivers remote:status to subscribers.
(function testMockFanout() {
  if (typeof window === "undefined") {
    (globalThis as Record<string, unknown>).window = {} as Window & typeof globalThis;
  }
  let received: RemoteConnectionStatus | null = null;
  const off = onRemoteStatus((s) => {
    received = s;
  });
  __emitMockRemote("status", { hostId: "z", state: "connected" });
  eq(received !== null && (received as RemoteConnectionStatus).hostId, "z", "mock fan-out delivers status");
  off();
  received = null;
  __emitMockRemote("status", { hostId: "y", state: "connected" });
  eq(received, null, "unsubscribe stops delivery");
})();

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
