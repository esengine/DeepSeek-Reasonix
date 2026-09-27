import { describe, expect, it } from "vitest";
import { RemoteDeviceRegisterSchema, RemoteGrantIssueSchema } from "./validation";

describe("remote access validation", () => {
  it("accepts a registered device and removes duplicate capabilities", () => {
    const parsed = RemoteDeviceRegisterSchema.parse({
      name: " Home Mac ",
      platform: "macos",
      publicKey: "A".repeat(43),
      capabilities: ["terminal", "terminal", "logs"],
    });
    expect(parsed.name).toBe("Home Mac");
    expect(parsed.capabilities).toEqual(["terminal", "logs"]);
  });

  it("rejects unknown capabilities and malformed device identifiers", () => {
    expect(() => RemoteGrantIssueSchema.parse({
      targetDeviceId: "short",
      scopes: ["shell"],
    })).toThrow();
  });
});
