import { test } from "node:test";
import assert from "node:assert/strict";
import { safeNext } from "./safe-next.js";

const ORIGIN = "https://reasonix.io";

// The security invariant: whatever safeNext returns, re-resolving it the way a
// browser does when it assigns `location.href` must stay on our origin or an
// allowed reasonix.io host. Asserting this (rather than a fixed string) catches
// any value that looks same-origin but re-parses off-site.
function assertLandsSomewhereSafe(returned) {
  if (returned === null) return;
  const landed = new URL(returned, ORIGIN);
  const ok = landed.origin === ORIGIN || landed.host === "reasonix.io" || landed.host.endsWith(".reasonix.io");
  assert.ok(ok, `returned ${JSON.stringify(returned)} re-resolves off-site to ${landed.href}`);
}

test("same-origin path passes through as an absolute same-origin URL", () => {
  const got = safeNext("/account/settings?x=1#y", ORIGIN);
  assert.equal(got, "https://reasonix.io/account/settings?x=1#y");
  assertLandsSomewhereSafe(got);
});

test("allowed reasonix.io subdomain passes through", () => {
  const got = safeNext("https://crash.reasonix.io/x", ORIGIN);
  assert.equal(got, "https://crash.reasonix.io/x");
  assertLandsSomewhereSafe(got);
});

test("empty/missing next", () => {
  assert.equal(safeNext("", ORIGIN), null);
  assert.equal(safeNext(null, ORIGIN), null);
});

test("rejects a plain protocol-relative redirect", () => {
  assert.equal(safeNext("//evil.example", ORIGIN), null);
});

test("rejects a backslash-prefixed redirect", () => {
  assert.equal(safeNext("/\\evil.example", ORIGIN), null);
  assert.equal(safeNext("/\\/evil.example", ORIGIN), null);
});

test("rejects control characters (as decoded by URLSearchParams) smuggling a protocol-relative redirect", () => {
  // URLSearchParams.get() decodes %09/%0A/%0D before this function ever sees
  // the value, e.g. "?next=/%09/evil.example" arrives as "/\t/evil.example".
  for (const c of ["\t", "\n", "\r"]) {
    assert.equal(safeNext(`/${c}/evil.example`, ORIGIN), null, JSON.stringify(c));
  }
});

test("dot-segment inputs that normalize to a //host pathname do not escape", () => {
  // These resolve to a same-origin URL whose pathname is "//evil.example".
  // Returning the bare pathname would re-parse as a protocol-relative redirect;
  // returning the absolute href keeps them on reasonix.io. Assert end to end.
  for (const p of ["/.//evil.example", "/a/..//evil.example", "/..//evil.example", "/%2e//evil.example"]) {
    const got = safeNext(decodeURIComponent(p), ORIGIN);
    assert.notEqual(got, null, p);
    assert.equal(new URL(got, ORIGIN).origin, ORIGIN, `${p} -> ${got}`);
    assertLandsSomewhereSafe(got);
  }
});

test("rejects an unrelated https host and non-https schemes", () => {
  assert.equal(safeNext("https://evil.example/", ORIGIN), null);
  assert.equal(safeNext("javascript:alert(1)", ORIGIN), null);
});

test("rejects a reasonix.io-lookalike host", () => {
  assert.equal(safeNext("https://reasonix.io.evil.example/", ORIGIN), null);
});
