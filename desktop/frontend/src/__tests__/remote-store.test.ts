// Run: tsx src/__tests__/remote-store.test.ts
//
// Tests for the remote store's status/fingerprint reconciliation and the
// bridge mock's remote:* event fan-out.

import { RemoteConnectionTimeoutError, useRemoteStore, waitForRemoteConnection } from "../store/remote";
import { onRemoteStatus, __emitMockRemote } from "../lib/bridge";
import { isRemoteHostKeyMismatch, remoteConnectionErrorSummaryKey } from "../lib/remoteErrors";
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
  useRemoteStore.setState({ hosts: [], statuses: {}, pendingFingerprint: null, pendingSecretPrompt: null, statusPopoverRequest: null });
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

// Interactive credentials expose metadata to the UI, never the secret value.
reset();
useRemoteStore.getState().applyStatus({
  hostId: "box",
  state: "pending_secret",
  secretPrompt: { promptId: "prompt-1", hostId: "box", host: "dev@box.test", kind: "password" },
});
eq(useRemoteStore.getState().pendingSecretPrompt?.kind, "password", "pending_secret opens the one-shot credential dialog");
eq(JSON.stringify(useRemoteStore.getState().statuses.box).includes("secret-value"), false, "status contains no credential plaintext");
const oldSecretPrompt = useRemoteStore.getState().pendingSecretPrompt!;
useRemoteStore.getState().applyStatus({
  hostId: "other",
  state: "pending_secret",
  secretPrompt: { promptId: "prompt-2", hostId: "other", host: "other.test", kind: "passphrase" },
});
useRemoteStore.getState().clearPendingSecretPrompt(oldSecretPrompt);
eq(useRemoteStore.getState().pendingSecretPrompt?.hostId, "other", "stale credential dialog cannot clear a newer prompt");
const firstIdentityPrompt = {
  promptId: "prompt-3", hostId: "other", host: "other.test", kind: "passphrase" as const, identity: "id_first",
};
useRemoteStore.getState().applyStatus({ hostId: "other", state: "pending_secret", secretPrompt: firstIdentityPrompt });
useRemoteStore.getState().applyStatus({
  hostId: "other",
  state: "pending_secret",
  secretPrompt: { promptId: "prompt-4", hostId: "other", host: "other.test", kind: "passphrase", identity: "id_second" },
});
useRemoteStore.getState().clearPendingSecretPrompt(firstIdentityPrompt);
eq(useRemoteStore.getState().pendingSecretPrompt?.identity, "id_second", "one key prompt cannot clear the next key prompt");
const oldSameMetadataPrompt = useRemoteStore.getState().pendingSecretPrompt!;
useRemoteStore.getState().applyStatus({
  hostId: "other",
  state: "pending_secret",
  secretPrompt: { promptId: "prompt-5", hostId: "other", host: "other.test", kind: "passphrase", identity: "id_second" },
});
useRemoteStore.getState().clearPendingSecretPrompt(oldSameMetadataPrompt);
eq(useRemoteStore.getState().pendingSecretPrompt?.promptId, "prompt-5", "opaque prompt ID protects sequential prompts with identical metadata");
useRemoteStore.getState().applyStatus({ hostId: "other", state: "connecting" });
eq(useRemoteStore.getState().pendingSecretPrompt, null, "credential resolution clears the prompt");

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

useRemoteStore.getState().requestStatusPopover("box");
const firstReveal = useRemoteStore.getState().statusPopoverRequest!;
eq(firstReveal.hostId, "box", "connection failures can request the anchored status popover");
useRemoteStore.getState().requestStatusPopover("box");
const secondReveal = useRemoteStore.getState().statusPopoverRequest!;
eq(secondReveal.nonce > firstReveal.nonce, true, "repeated failures create a fresh popover request");
useRemoteStore.getState().clearStatusPopoverRequest(firstReveal);
eq(useRemoteStore.getState().statusPopoverRequest?.nonce, secondReveal.nonce, "stale popover completion cannot clear a newer request");
useRemoteStore.getState().clearStatusPopoverRequest(secondReveal);
eq(useRemoteStore.getState().statusPopoverRequest, null, "matching popover request is consumed");

const mismatchStatus: RemoteConnectionStatus = {
  hostId: "box",
  state: "stopped",
  error: "raw path-bearing backend error",
  errorDetails: {
    code: "host_key_mismatch",
    presentedSha256: "SHA256:new",
    knownHostRecords: [{ path: "/home/dev/.ssh/known_hosts", line: 7 }],
  },
};
eq(isRemoteHostKeyMismatch(mismatchStatus), true, "structured mismatch is recognized without parsing raw error text");
eq(remoteConnectionErrorSummaryKey(mismatchStatus), "remote.error.summary.host_key_mismatch", "mismatch uses the localized safe summary");
eq(
  remoteConnectionErrorSummaryKey({ hostId: "legacy", state: "degraded", error: "forward attach failed" }),
  "remote.error.summary.degraded",
  "legacy degraded status falls back to the warning summary",
);

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

useRemoteStore.getState().applyStatus({ hostId: "timeout", state: "connecting" });
let timeoutError: unknown;
try {
  await waitForRemoteConnection("timeout", 1);
} catch (err) {
  timeoutError = err;
}
eq(timeoutError instanceof RemoteConnectionTimeoutError, true, "connection waiter exposes a typed timeout for recovery UI");
eq(useRemoteStore.getState().statuses.timeout?.state, "connecting", "connection timeout does not forge a backend terminal state");

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
