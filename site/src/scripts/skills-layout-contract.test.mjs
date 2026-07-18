import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = () => readFile(new URL("../pages/skills.astro", import.meta.url), "utf8");

test("the registry publish CTA does not inherit the publish panel layout", async () => {
  const page = await source();

  assert.match(page, /class="btn btn-dark reg-publish-cta" id="publish-open"/);
  assert.match(page, /<section class="reg-publish" id="publish" hidden>/);
  assert.doesNotMatch(page, /class="btn btn-dark reg-publish" id="publish-open"/);
});
