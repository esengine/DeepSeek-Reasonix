import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { EASE_OUT, toCssEasing } from "../lib/gsapAnimations";

const CSS_EASING_RE = /^(ease|linear|ease-in|ease-out|ease-in-out|step-start|step-end|steps\([^)]*\)|cubic-bezier\([^)]*\))$/;

describe("toCssEasing", () => {
  it("maps GSAP power2.out to the app-wide CSS cubic-bezier", () => {
    assert.equal(toCssEasing("power2.out"), "cubic-bezier(0.2, 0.72, 0.2, 1)");
    assert.equal(toCssEasing(EASE_OUT), "cubic-bezier(0.2, 0.72, 0.2, 1)");
  });

  it("maps GSAP power2.in to a CSS easing the Web Animations API accepts", () => {
    const result = toCssEasing("power2.in");
    // Regression: passing "power2.in" straight to el.animate throws
    // TypeError ('power2.in' is not a valid value for easing), which crashed
    // the approval card exit animation (animateShelfExit -> answerWithExit).
    assert.equal(result, "cubic-bezier(0.55, 0.06, 0.68, 0.19)");
    assert.match(result, CSS_EASING_RE);
  });

  it("passes through valid CSS easing values unchanged", () => {
    for (const value of ["ease", "linear", "ease-in", "ease-out", "ease-in-out", "cubic-bezier(0.2, 0.72, 0.2, 1)"]) {
      assert.equal(toCssEasing(value), value);
      assert.match(toCssEasing(value), CSS_EASING_RE);
    }
  });

  it("passes through unknown values unchanged", () => {
    assert.equal(toCssEasing(""), "");
    assert.equal(toCssEasing("back.out"), "back.out");
  });
});
