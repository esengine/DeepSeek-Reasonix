import { describe, expect, it } from "vitest";
import { controllerMessage, offeredProtocols, parseDeviceReply } from "./protocol";

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

describe("WebSocket protocol offers", () => {
  it("parses the browser's ordered protocol list", () => {
    const request = new Request("https://remote.reasonix.io/v1/sessions/connect", {
      headers: { "sec-websocket-protocol": "reasonix.remote.v1, reasonix.auth.ticket" },
    });
    expect(offeredProtocols(request)).toEqual(["reasonix.remote.v1", "reasonix.auth.ticket"]);
  });
});
