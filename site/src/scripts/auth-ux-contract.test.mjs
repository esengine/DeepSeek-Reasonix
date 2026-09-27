import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");

test("password fields use the accessible visibility toggle", async () => {
  const [component, script, ...pages] = await Promise.all([
    read("components/PasswordInput.astro"),
    read("scripts/auth.js"),
    ...["pages/login.astro", "pages/register.astro", "pages/reset.astro", "pages/account.astro"].map(read),
  ]);
  assert.match(component, /data-password-toggle/);
  assert.match(component, /aria-controls=\{id\}/);
  assert.match(script, /input\.type = show \? "text" : "password"/);
  pages.forEach((page) => assert.match(page, /<PasswordInput/));
});

test("preset device codes skip the redundant continue step", async () => {
  const [page, script] = await Promise.all([read("pages/device.astro"), read("scripts/auth.js")]);
  assert.match(page, /id="device-code-step"/);
  assert.match(page, /批准登录/);
  assert.match(script, /if \(codeStep\) codeStep\.hidden = true/);
  assert.match(script, /Reasonix Studio 重新发起登录/);
  assert.match(script, /startGrantExpiry\(d\.grant\.expiresAt\)/);
  assert.match(script, /grantExpiryTimer = setInterval\(render, 1000\)/);
  assert.doesNotMatch(script, /setInterval\(showGrant/);
  assert.doesNotMatch(script, /Approved\. Return to your terminal/);
});
