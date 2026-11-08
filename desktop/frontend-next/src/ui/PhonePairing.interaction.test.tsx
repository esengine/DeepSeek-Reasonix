// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import { PhonePop } from "./PhonePop";

const api = vi.hoisted(() => ({ canScanPairingCode: () => true, scanPairingCode: vi.fn(), openInApp: vi.fn() }));
vi.mock("../port/host", () => ({ host: () => api }));
afterEach(cleanup);
beforeEach(() => { api.scanPairingCode.mockReset(); api.openInApp.mockReset(); });

it("returns from a cancelled camera to a usable connection card and can scan again", async () => {
  let finish: (value: string) => void = () => {};
  api.scanPairingCode.mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; })).mockResolvedValueOnce("");
  render(<PhonePop hub={new MockHub()} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "扫码连接电脑" }));
  const scan = screen.getByRole("button", { name: "打开相机扫码" });
  await user.click(scan);
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  finish("");
  await waitFor(() => expect((scan as HTMLButtonElement).disabled).toBe(false));
  expect(api.openInApp).not.toHaveBeenCalled();
  expect(screen.queryByRole("alert")).toBeNull();
  await user.click(scan);
  await waitFor(() => expect(api.scanPairingCode).toHaveBeenCalledTimes(2));
});

it("shows the scanned computer before navigating and keeps an invalid code on the same page", async () => {
  api.scanPairingCode.mockResolvedValueOnce("javascript:alert(1)").mockResolvedValueOnce("http://192.168.1.2:4321/#pair=test");
  render(<PhonePop hub={new MockHub()} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "扫码连接电脑" }));
  await user.click(screen.getByRole("button", { name: "打开相机扫码" }));
  await screen.findByRole("alert");
  expect(api.openInApp).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "打开相机扫码" }));
  await screen.findByText("http://192.168.1.2:4321");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(api.openInApp).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "连接这台电脑" }));
  expect(api.openInApp).toHaveBeenCalledWith("http://192.168.1.2:4321/#pair=test");
});
