import { describe, expect, it, vi } from "vitest";
import {
  type RemoteChannelLike,
  pickRemoteChannel,
  relayRemoteSlashInfo,
} from "../src/cli/ui/remote-channel.js";

function makeChannel(): RemoteChannelLike {
  return {
    sendText: vi.fn(),
    sendInfo: vi.fn(),
    handleRemoteSlashResult: vi.fn(() => false),
  };
}

describe("remote channel helpers", () => {
  it("picks the matching remote channel for qq and feishu", () => {
    const qq = makeChannel();
    const feishu = makeChannel();

    expect(pickRemoteChannel("qq", qq, feishu)).toBe(qq);
    expect(pickRemoteChannel("feishu", qq, feishu)).toBe(feishu);
    expect(pickRemoteChannel(null, qq, feishu)).toBeNull();
  });

  it("relays sync slash info only when a remote channel exists", () => {
    const qq = makeChannel();

    relayRemoteSlashInfo(qq, "status text");
    expect(qq.sendText).toHaveBeenCalledWith("status text");

    relayRemoteSlashInfo(null, "ignored");
    relayRemoteSlashInfo(qq, undefined);
    expect(qq.sendText).toHaveBeenCalledTimes(1);
  });
});
