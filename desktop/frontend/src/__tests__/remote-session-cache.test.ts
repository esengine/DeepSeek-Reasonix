// Run: tsx src/__tests__/remote-session-cache.test.ts

import { JSDOM } from "jsdom";
import {
  loadRemoteSessionCache,
  REMOTE_SESSION_CACHE_TTL_MS,
  removeRemoteSessionCache,
  saveRemoteSessionCache,
} from "../lib/remoteSessionCache";
import type { RemoteSessionView } from "../lib/types";

let passed = 0;
function ok(value: boolean, label: string): void {
  if (!value) throw new Error(label);
  passed += 1;
  process.stdout.write(`  PASS  ${label}\n`);
}

const dom = new JSDOM("<!doctype html>", { url: "http://localhost/" });
globalThis.localStorage = dom.window.localStorage;

const key = "box\u0000~/app";
const rows: RemoteSessionView[] = [{ name: "one", path: "/sessions/one.jsonl", title: "One", turns: 2 }];
const now = 10_000;

saveRemoteSessionCache(key, rows, now);
ok(loadRemoteSessionCache(key, now + 1)[0]?.name === "one", "restores a current versioned snapshot");

saveRemoteSessionCache(key, [
  { name: "", path: "/sessions/transient-blank.jsonl", title: "", turns: 0, current: true },
  ...rows,
], now);
const withoutBlank = loadRemoteSessionCache(key, now + 1);
ok(withoutBlank.length === 1 && withoutBlank[0]?.name === "one",
  "does not persist a transient blank session with a generated path");

ok(loadRemoteSessionCache(key, now + REMOTE_SESSION_CACHE_TTL_MS + 1).length === 0, "expires stale snapshots");
ok(localStorage.length === 0, "removes an expired snapshot from storage");

localStorage.setItem("projectTree:remoteSessions:" + key, JSON.stringify(rows));
ok(loadRemoteSessionCache(key, now).length === 0, "invalidates the legacy unversioned array schema");
ok(localStorage.length === 0, "removes an invalid legacy snapshot");

localStorage.setItem("projectTree:remoteSessions:" + key, JSON.stringify({
  version: 1,
  savedAt: now,
  rows: [{ name: "bad", title: "Bad", turns: 1, running: "yes" }],
}));
ok(loadRemoteSessionCache(key, now).length === 0, "rejects malformed optional row fields");
ok(localStorage.length === 0, "removes a malformed versioned snapshot");

localStorage.setItem("projectTree:remoteSessions:" + key, JSON.stringify({
  version: 1,
  savedAt: now,
  rows: [{ name: "empty" }],
}));
const empty = loadRemoteSessionCache(key, now);
ok(empty.length === 1 && empty[0]?.title === "" && empty[0]?.turns === 0,
  "normalizes Go-omitted zero-value title and turns");

saveRemoteSessionCache(key, rows, now);
removeRemoteSessionCache(key);
ok(loadRemoteSessionCache(key, now).length === 0, "explicit removal clears an unpinned project cache");

console.log(`\n${passed} remote session cache assertions passed.`);
