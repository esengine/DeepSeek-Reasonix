import { describe, expect, it } from "vitest";
import { controllerMessage, parseDeviceReply } from "./protocol";

describe("remote message routing envelope", () => {
  it("carries connection scopes outside the opaque payload", () => {
    expect(JSON.parse(controllerMessage("a".repeat(32), ["terminal"], "encrypted-body"))).toEqual({
      type: "controller_message",
      connectionId: "a".repeat(32),
      scopes: ["terminal"],
      payload: "encrypted-body",
    });
  });

  it("accepts only a directed device reply", () => {
    expect(parseDeviceReply(JSON.stringify({ to: "b".repeat(32), payload: "encrypted-response" }))).toEqual({
      to: "b".repeat(32),
      payload: "encrypted-response",
    });
    expect(parseDeviceReply(JSON.stringify({ payload: "broadcast" }))).toBeNull();
    expect(parseDeviceReply("not-json")).toBeNull();
  });
});
