import { describe, expect, it } from "vitest";
import { decideTelegramAccess, describeTelegramAccess } from "../src/telegram/access.js";

describe("Telegram access control", () => {
  it("fails closed when no owner or allowlist is configured", () => {
    expect(decideTelegramAccess({}, "1001")).toEqual({
      accept: false,
      reason: "unauthorized",
    });
    expect(describeTelegramAccess({})).toBe("access control required");
  });

  it("accepts allowlist members without binding the first sender", () => {
    expect(decideTelegramAccess({ allowlist: ["1001", "1002"] }, "1002")).toEqual({
      accept: true,
      mode: "allowlist",
      bindRuntime: false,
    });
  });

  it("rejects non-matching senders once an owner is configured", () => {
    expect(decideTelegramAccess({ ownerUserId: "1001" }, "2002")).toEqual({
      accept: false,
      reason: "unauthorized",
    });
  });
});
