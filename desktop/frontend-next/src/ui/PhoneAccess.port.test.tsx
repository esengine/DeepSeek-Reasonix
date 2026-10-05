// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PhoneAccess } from "./PhoneAccess";
import type { HubPort } from "../port/hub";
import type { ShareStatus } from "../port/share";

afterEach(cleanup);

const base: ShareStatus = {
  open: false, addresses: [{ ip: "10.0.0.2", interface: "en0", kind: "lan" }], devices: [], cloudDevices: [],
};

function rig(initial: ShareStatus) {
  let st = initial;
  const hub = {
    shareStatus: vi.fn(async () => st),
    setSharePort: vi.fn(async (port: number) => (st = { ...st, port: port || undefined })),
    openShare: vi.fn(async (ip: string) => (st = { ...st, open: true, origin: `http://${ip}:${st.port ?? 50000}` })),
    offerShare: vi.fn(async () => ({ url: "", qr: "<svg/>", expires: new Date(Date.now() + 60_000).toISOString() })),
  };
  return { hub, ui: <PhoneAccess hub={hub as unknown as HubPort} onError={() => {}} /> };
}

const portField = () =>
  waitFor(() => {
    const el = document.querySelector<HTMLInputElement>('[data-action="share.port"]');
    expect(el).not.toBeNull();
    return el!;
  });

it("saves a typed port when the field loses focus and leaves a shut door shut", async () => {
  const { hub, ui } = rig(base);
  render(ui);
  const field = await portField();
  await userEvent.type(field, "41234");
  await userEvent.tab();
  await waitFor(() => expect(hub.setSharePort).toHaveBeenCalledWith(41234));
  expect(hub.openShare).not.toHaveBeenCalled();
});

it("clearing the field sends zero, and an unchanged field sends nothing", async () => {
  const { hub, ui } = rig({ ...base, port: 41234 });
  render(ui);
  const field = await portField();
  expect(field.value).toBe("41234");
  await userEvent.click(field);
  await userEvent.tab();
  expect(hub.setSharePort).not.toHaveBeenCalled();
  await userEvent.clear(field);
  await userEvent.tab();
  await waitFor(() => expect(hub.setSharePort).toHaveBeenCalledWith(0));
});

it("moves an open door onto the new port", async () => {
  const { hub, ui } = rig({ ...base, open: true, origin: "http://10.0.0.2:50000" });
  render(ui);
  const field = await portField();
  await userEvent.type(field, "41234");
  await userEvent.tab();
  await waitFor(() => expect(hub.openShare).toHaveBeenCalledWith("10.0.0.2"));
  expect(hub.setSharePort).toHaveBeenCalledWith(41234);
});
