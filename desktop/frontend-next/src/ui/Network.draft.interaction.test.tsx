// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Network } from "./Network";
import { MockPort } from "../port/mock";
import type { NetworkSettings } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const initial: NetworkSettings = {
  mode: "custom", type: "http", server: "proxy.example", port: 8080,
  effective: "http://proxy.example:8080", endpoint: "https://model.example/v1", hasPassword: true,
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (why: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function portWith(settings: NetworkSettings = initial) {
  const port = new MockPort();
  vi.spyOn(port, "network").mockResolvedValue(settings);
  vi.spyOn(port, "saveNetwork").mockImplementation(async (draft) => ({ ...draft, effective: "direct" }));
  vi.spyOn(port, "diagnoseNetwork").mockResolvedValue([{ step: "connect", ok: true, detail: "saved route tested", durationMs: 0 }]);
  return port;
}

const save = () => screen.getByRole<HTMLButtonElement>("button", { name: "保存" });

it("disables saving again when edits are reverted", async () => {
  const port = portWith();
  render(<Network port={port} />);
  const field = await screen.findByLabelText<HTMLInputElement>("服务器");
  await userEvent.type(field, ".draft");
  expect(save().disabled).toBe(false);
  await userEvent.clear(field);
  await userEvent.type(field, initial.server!);
  const clear = screen.getByRole("checkbox", { name: "删除已保存的密码" });
  await userEvent.click(clear);
  expect(save().disabled).toBe(false);
  await userEvent.click(clear);
  expect(save().disabled).toBe(true);
  expect(screen.queryByText(/未保存/)).toBeNull();
  await userEvent.click(save());
  expect(port.saveNetwork).not.toHaveBeenCalled();
});

it("distinguishes loading from a failed read", async () => {
  const port = portWith();
  const pending = deferred<NetworkSettings>();
  vi.mocked(port.network).mockReturnValue(pending.promise);
  render(<Network port={port} />);
  expect(screen.getByRole("status").textContent).toBe("正在读取网络配置…");
  await act(async () => pending.reject(new Error("network settings unavailable")));
  expect(screen.getByRole("alert").textContent).toBe("network settings unavailable");
  expect(screen.queryByRole("button", { name: "保存" })).toBeNull();
});

it("only saves changed fields and keeps the effective route tied to the saved state", async () => {
  const port = portWith();
  render(<Network port={port} />);
  await screen.findByRole("radio", { name: "直连" });
  expect(save().disabled).toBe(true);
  await userEvent.click(screen.getByRole("radio", { name: "直连" }));
  expect(save().disabled).toBe(false);
  expect(screen.getByText(initial.effective)).toBeTruthy();
  expect(screen.getByText(/未保存/)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "测试已保存配置" }));
  expect(port.diagnoseNetwork).toHaveBeenCalledExactlyOnceWith();
  expect(port.saveNetwork).not.toHaveBeenCalled();
  expect(await screen.findByText("saved route tested")).toBeTruthy();
  await userEvent.click(save());
  await waitFor(() => expect(save().disabled).toBe(true));
  expect(port.saveNetwork).toHaveBeenCalledWith(expect.objectContaining({ mode: "off" }), "", false);
  expect(screen.getByText("direct")).toBeTruthy();
  expect(screen.queryByText("saved route tested")).toBeNull();
});

it("freezes a submitted draft and preserves it after refusal", async () => {
  const port = portWith();
  const pending = deferred<NetworkSettings>();
  vi.mocked(port.saveNetwork).mockReturnValue(pending.promise);
  render(<Network port={port} />);
  const field = await screen.findByLabelText<HTMLInputElement>("服务器");
  await userEvent.type(field, ".new");
  await userEvent.click(save());
  expect(field.matches(":disabled")).toBe(true);
  expect(screen.getByRole("radio", { name: "直连" }).matches(":disabled")).toBe(true);
  await userEvent.click(screen.getByRole("radio", { name: "直连" }));
  await userEvent.type(field, "ignored");
  await act(async () => pending.reject(new Error("disk refused")));
  expect(await screen.findByText("disk refused")).toBeTruthy();
  expect(field.value).toBe("proxy.example.new");
  expect(screen.getByRole("radio", { name: "手动设置" }).getAttribute("aria-checked")).toBe("true");
  expect(save().disabled).toBe(false);
  expect(screen.getByText(initial.effective)).toBeTruthy();
});

it("lets a draft change during a test without relabelling the tested saved route", async () => {
  const port = portWith();
  const pending = deferred<Awaited<ReturnType<typeof port.diagnoseNetwork>>>();
  vi.mocked(port.diagnoseNetwork).mockReturnValue(pending.promise);
  render(<Network port={port} />);
  const field = await screen.findByLabelText<HTMLInputElement>("服务器");
  await userEvent.click(screen.getByRole("button", { name: "测试已保存配置" }));
  expect(field.matches(":disabled")).toBe(false);
  await userEvent.type(field, ".draft");
  await act(async () => pending.resolve([{ step: "connect", ok: true, detail: "original saved route", durationMs: 0 }]));
  expect(field.value).toBe("proxy.example.draft");
  expect(screen.getByText(initial.effective)).toBeTruthy();
  expect(screen.getByRole("region", { name: "已保存配置的测试结果" }).textContent).toContain("original saved route");
  expect(save().disabled).toBe(false);
});

it("keeps password replacement and explicit deletion distinct", async () => {
  const port = portWith();
  render(<Network port={port} />);
  const clear = await screen.findByRole("checkbox", { name: "删除已保存的密码" });
  await userEvent.click(clear);
  expect(save().disabled).toBe(false);
  await userEvent.click(save());
  await waitFor(() => expect(save().disabled).toBe(true));
  expect(port.saveNetwork).toHaveBeenLastCalledWith(expect.anything(), "", true);
  const password = screen.getByLabelText<HTMLInputElement>("密码（可选）");
  await userEvent.type(password, "replacement-fixture");
  await userEvent.click(save());
  await waitFor(() => expect(password.value).toBe(""));
  expect(port.saveNetwork).toHaveBeenLastCalledWith(expect.anything(), "replacement-fixture", false);
});

it.each(["load", "save", "diagnose"] as const)("drops an old port's pending %s result", async (operation) => {
  const a = portWith(), b = portWith({ mode: "off", effective: "B direct" });
  const pending = deferred<NetworkSettings>();
  const probe = deferred<Awaited<ReturnType<typeof a.diagnoseNetwork>>>();
  if (operation === "load") vi.mocked(a.network).mockReturnValue(pending.promise);
  if (operation === "save") vi.mocked(a.saveNetwork).mockReturnValue(pending.promise);
  if (operation === "diagnose") vi.mocked(a.diagnoseNetwork).mockReturnValue(probe.promise);
  const view = render(<Network port={a} />);
  if (operation !== "load") {
    await screen.findByRole("radio", { name: "直连" });
    if (operation === "save") {
      await userEvent.click(screen.getByRole("radio", { name: "直连" }));
      await userEvent.click(save());
    } else await userEvent.click(screen.getByRole("button", { name: "测试已保存配置" }));
  }
  view.rerender(<Network port={b} />);
  await screen.findByText("B direct");
  await act(async () => {
    pending.resolve({ ...initial, effective: "A late" });
    probe.resolve([{ step: "connect", ok: true, detail: "A late probe", durationMs: 0 }]);
  });
  expect(screen.queryByText("A late")).toBeNull();
  expect(screen.queryByText("A late probe")).toBeNull();
  expect(screen.getByRole("radio", { name: "直连" }).getAttribute("aria-checked")).toBe("true");
  expect(save().disabled).toBe(true);
});
