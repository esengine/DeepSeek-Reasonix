// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PhonePop } from "./PhonePop";
import { HttpError } from "../port/http_error";
import type { HubPort } from "../port/hub";
import type { ShareStatus } from "../port/share";

afterEach(cleanup);

const status = (open: boolean): ShareStatus => ({
  open, addresses: [{ ip: "10.0.0.2", interface: "en0", kind: "lan" }], devices: [], cloudDevices: [],
});

function hubWith(reads: ShareStatus[], offerShare: () => Promise<unknown>) {
  let at = 0;
  const shareStatus = vi.fn(async () => reads[Math.min(at++, reads.length - 1)]);
  return { hub: { shareStatus, offerShare: vi.fn(offerShare) } as unknown as HubPort, shareStatus };
}

const closed = new HttpError(409, "share closed", { code: "share.closed" });

// The door was open when the window last looked and shut elsewhere since:
// opening the card reads first and does not ask a shut door for a code.
it("does not ask for a code when the read on opening finds the door shut", async () => {
  // Read on mount, read again as the open door starts being watched, then
  // shut by the time the card opens.
  const { hub, shareStatus } = hubWith([status(true), status(true), status(false)], () => Promise.reject(closed));
  render(<PhonePop hub={hub} />);
  await waitFor(() => expect(shareStatus).toHaveBeenCalledTimes(2));
  await userEvent.click(screen.getByRole("button", { name: "设备访问" }));
  await waitFor(() => expect(shareStatus).toHaveBeenCalledTimes(3));
  expect(hub.offerShare).not.toHaveBeenCalled();
  expect(screen.queryByRole("alert")).toBeNull();
});

// A failure in the card is said in the card, not somewhere else on screen.
it("says a failed code request inside the card", async () => {
  const { hub } = hubWith([status(true)], () => Promise.reject(closed));
  render(<PhonePop hub={hub} />);
  await userEvent.click(await screen.findByRole("button", { name: "设备访问" }));
  const dialog = await screen.findByRole("dialog");
  await waitFor(() => expect(dialog.querySelector('[role="alert"]')?.textContent).toBe("手机访问已关闭，请先开启。"));
});

it("disconnects an Internet controller after the same two-step confirmation", async () => {
  const remote = {
    ...status(false),
    cloudDevices: [{ id: "cloud-a", name: "Web Studio", connectedAt: new Date().toISOString(), lastSeen: new Date().toISOString(), ordinal: 1 }],
  };
  const revokeDevice = vi.fn(async () => ({ ...remote, cloudDevices: [] }));
  const hub = {
    shareStatus: vi.fn(async () => remote),
    offerShare: vi.fn(),
    revokeDevice,
  } as unknown as HubPort;
  render(<PhonePop hub={hub} />);
  await userEvent.click(await screen.findByRole("button", { name: "设备访问" }));
  const disconnect = await screen.findByRole("button", { name: "断开" });
  await userEvent.click(disconnect);
  expect(screen.getByRole("button", { name: "确认断开" })).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "确认断开" }));
  await waitFor(() => expect(revokeDevice).toHaveBeenCalledWith("cloud-a"));
});

it("shows the account-gated Internet QR before the optional LAN pairing code", async () => {
  const remote = {
    ...status(false),
    cloudRemote: { deviceId: "device-a", name: "Home Mac", online: true },
  };
  const offerCloudShare = vi.fn(async () => ({
    url: "https://reasonix.io/remote/?device=device-a",
    qr: '<svg xmlns="http://www.w3.org/2000/svg"/>',
  }));
  const offerShare = vi.fn();
  const hub = {
    shareStatus: vi.fn(async () => remote),
    offerCloudShare,
    offerShare,
  } as unknown as HubPort;
  render(<PhonePop hub={hub} />);
  await userEvent.click(await screen.findByRole("button", { name: "设备访问" }));
  expect(await screen.findByRole("img", { name: "互联网连接二维码" })).toBeTruthy();
  expect(screen.getByText("手机扫码 · 不在同一网络也能连接")).toBeTruthy();
  expect(offerCloudShare).toHaveBeenCalledTimes(1);
  expect(offerShare).not.toHaveBeenCalled();
});
