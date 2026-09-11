import assert from "node:assert/strict";
import { test } from "node:test";
import { browserLayoutInDIP } from "./layout.js";
import { validateLayout } from "./surfaceManager.js";

test("CSS browser bounds match native bounds at enlarged and reduced app zoom", () => {
  const css = { x: 400, y: 100, width: 300, height: 250 };
  assert.deepEqual(browserLayoutInDIP(css, 1.5), { x: 600, y: 150, width: 450, height: 375 });
  assert.deepEqual(browserLayoutInDIP(css, 0.8), { x: 320, y: 80, width: 240, height: 200 });
  assert.deepEqual(validateLayout(browserLayoutInDIP(css, 1.5)!, { width: 1000, height: 800 }),
    { x: 600, y: 150, width: 400, height: 375 }, "native window clipping happens after conversion");
  assert.equal(browserLayoutInDIP(null, 1.5), null);
  assert.throws(() => browserLayoutInDIP(css, 0), /zoom/);
});
