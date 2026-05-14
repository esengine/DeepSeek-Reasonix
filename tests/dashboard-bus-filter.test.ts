import { describe, expect, it } from "vitest";
import { isThirdPartyError } from "../dashboard/src/lib/bus-filter.js";

describe("isThirdPartyError", () => {
  it("flags errors with a chrome-extension:// frame in the stack (real #818 case)", () => {
    const err = new Error("require is not defined");
    err.stack = [
      "ReferenceError: require is not defined",
      "    at Proxy.<anonymous> (chrome-extension://iikmkjmpaadaobahmlepeloendndfphd/userscript.html?name=adblock-ublock-x:8:14)",
      "    at At (<anonymous>:10:89)",
    ].join("\n");
    expect(isThirdPartyError(err)).toBe(true);
  });

  it("flags errors whose filename argument points at a browser extension", () => {
    const err = new Error("something");
    expect(isThirdPartyError(err, "chrome-extension://abc/userscript.js")).toBe(true);
    expect(isThirdPartyError(err, "moz-extension://abc/script.js")).toBe(true);
    expect(isThirdPartyError(err, "safari-web-extension://abc/script.js")).toBe(true);
  });

  it("does not flag dashboard-origin errors", () => {
    const err = new Error("real bug");
    err.stack = [
      "Error: real bug",
      "    at chat.ts (http://localhost:5174/src/panels/chat.ts:123:4)",
      "    at preact.module.js (http://localhost:5174/node_modules/preact/dist/preact.module.js:99:1)",
    ].join("\n");
    expect(isThirdPartyError(err)).toBe(false);
  });

  it("does not crash on non-Error values (rejection with string / null)", () => {
    expect(isThirdPartyError("plain string")).toBe(false);
    expect(isThirdPartyError(null)).toBe(false);
    expect(isThirdPartyError(undefined)).toBe(false);
    expect(isThirdPartyError({ no: "stack" })).toBe(false);
  });

  it("flags rejections (Promise.reject(error)) when the reason's stack mentions an extension", () => {
    const reason = new Error("rejected from a userscript");
    reason.stack =
      "Error: rejected from a userscript\n    at chrome-extension://iikmkjmpaadaobahmlepeloendndfphd/content.js:9:1303";
    expect(isThirdPartyError(reason)).toBe(true);
  });
});
