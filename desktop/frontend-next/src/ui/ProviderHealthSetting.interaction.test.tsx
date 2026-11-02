// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ProviderHealthSetting } from "./ProviderHealthSetting";
import { useModelHealth } from "./modelhealth";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import { t } from "../i18n";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const models = [{ ref: "one/alpha", provider: "one", model: "alpha" }];

it.each([404, 405, 501])("explains an older server refusing health settings with %s", async (status) => {
  const port = new MockPort();
  vi.spyOn(port, "providerHealthSettings").mockRejectedValue(new HttpError(status, "unsupported"));
  const save = vi.spyOn(port, "saveProviderHealthSettings");
  render(<ProviderHealthSetting port={port} onChanged={vi.fn()} onFailed={vi.fn()} />);
  await screen.findByText(t("当前服务器不支持自动检测，请升级服务器后重试。"));
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("false");
  expect(screen.getByRole("switch").hasAttribute("disabled")).toBe(true);
  expect(save).not.toHaveBeenCalled();
});

it("explains a missing save capability and distinguishes a settings read failure", async () => {
  const port = new MockPort();
  const view = render(<ProviderHealthSetting port={{ providerHealthSettings: () => port.providerHealthSettings() }} onChanged={vi.fn()} onFailed={vi.fn()} />);
  await screen.findByText(t("当前服务器不支持自动检测，请升级服务器后重试。"));
  vi.spyOn(port, "providerHealthSettings").mockRejectedValue(new HttpError(403, "forbidden"));
  view.rerender(<ProviderHealthSetting port={port} onChanged={vi.fn()} onFailed={vi.fn()} />);
  await screen.findByText(t("无法读取检测设置，请检查连接或权限后重试。"));
  expect(screen.getByRole("switch").hasAttribute("disabled")).toBe(true);
  expect(screen.queryByText(t("当前服务器不支持自动检测，请升级服务器后重试。"))).toBeNull();
});

it("requires opt-in, persists both switch directions and stops probes after disabling", async () => {
  const port = new MockPort(), changed = vi.fn(), failed = vi.fn();
  const probe = vi.spyOn(port, "providerHealth");
  const saved = vi.spyOn(port, "saveProviderHealthSettings");
  const { result, rerender } = renderHook(({ pulse }) => useModelHealth(port, pulse), { initialProps: { pulse: 0 } });
  render(<ProviderHealthSetting port={port} onChanged={changed} onFailed={failed} />);
  const toggle = screen.getByRole("switch");
  await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false));
  expect(toggle.getAttribute("aria-checked")).toBe("false");
  await act(() => result.current.check(Promise.resolve(models)));
  expect(probe).not.toHaveBeenCalled();
  fireEvent.click(toggle);
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(1));
  expect(saved).toHaveBeenLastCalledWith(true);
  await act(() => result.current.check(Promise.resolve(models)));
  expect(probe).toHaveBeenCalledTimes(1);
  fireEvent.click(toggle);
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(2));
  expect(saved).toHaveBeenLastCalledWith(false);
  rerender({ pulse: 1 });
  await act(() => result.current.check(Promise.resolve(models)));
  expect(probe).toHaveBeenCalledTimes(1);
  expect(result.current.readings).toEqual({});
  expect(failed).not.toHaveBeenCalled();
});

it("fails closed on unavailable settings and ignores late consent after a runtime switch", async () => {
  const old = new MockPort(), port = new MockPort();
  let finish!: (value: { enabled: boolean }) => void;
  vi.spyOn(old, "providerHealthSettings").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  vi.spyOn(port, "providerHealthSettings").mockRejectedValue(new Error("unsupported"));
  const oldProbe = vi.spyOn(old, "providerHealth"), probe = vi.spyOn(port, "providerHealth");
  const { result, rerender } = renderHook(({ port }) => useModelHealth(port, 0), { initialProps: { port: old } });
  let pending!: Promise<void>;
  act(() => { pending = result.current.check(Promise.resolve(models)); });
  rerender({ port });
  await act(async () => { finish({ enabled: true }); await pending; await result.current.check(Promise.resolve(models)); });
  expect(oldProbe).not.toHaveBeenCalled();
  expect(probe).not.toHaveBeenCalled();
  expect(result.current.readings).toEqual({});
});
