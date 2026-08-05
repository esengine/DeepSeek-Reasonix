import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const auth = readFileSync(new URL("./auth.js", import.meta.url), "utf8");
const community = readFileSync(new URL("./community.js", import.meta.url), "utf8");
const account = readFileSync(new URL("../pages/account.astro", import.meta.url), "utf8");
const profile = readFileSync(new URL("../pages/u/index.astro", import.meta.url), "utf8");

test("unverified accounts can request a fresh verification email", () => {
  assert.match(account, /resend-verification-btn/);
  assert.match(auth, /\/auth\/resend-verification/);
});

test("public profiles have a generated site route backed by the public account API", () => {
  assert.match(profile, /public-profile/);
  assert.match(auth, /api\("\/u\/" \+ encodeURIComponent\(handle\)\)/);
});

test("community renders public handles and wires reactions", () => {
  assert.doesNotMatch(community, /split\(["']@["']\)/);
  assert.match(community, /\/posts\/\$\{like\.dataset\.like\}\/likes/);
  assert.match(community, /aria-pressed/);
});
