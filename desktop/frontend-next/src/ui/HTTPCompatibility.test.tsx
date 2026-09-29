// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderEntry } from "../port/port";
import type { Port } from "./Providers";
import { EditConn } from "./EditConn";
import { AddProvider } from "./AddProvider";

afterEach(cleanup);

it("loads the saved policy, probes a draft, and explicitly resets without replaying", async () => {
  const editProvider = vi.fn(async () => {});
  const checkProvider = vi.fn(async () => ({ ok: true, models: ["model-a"] }));
  const port = { editProvider, checkProvider } as unknown as Port;
  const entry: ProviderEntry = {
    name: "fixture", kind: "openai", baseUrl: "https://fixture.invalid/v1",
    models: ["model-a"], default: "model-a", hasKey: true, inUse: false,
    preset: false, http1Only: true,
  };
  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} />);
  await userEvent.click(screen.getByText("连接兼容与推理设置"));
  const selector = await screen.findByRole("combobox", { name: /^HTTP 连接协议/ });
  expect((selector as HTMLSelectElement).value).toBe("http1");
  await userEvent.selectOptions(selector, "auto");
  expect(editProvider).not.toHaveBeenCalled();
  expect(checkProvider).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "刷新模型目录" }));
  await waitFor(() => expect(checkProvider).toHaveBeenCalledWith("fixture", false));
  expect(editProvider).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ http1Only: false })));
  expect(checkProvider).toHaveBeenCalledTimes(1);
});

it("keeps HTTP compatibility in collapsed advanced options and saves an explicit selection", async () => {
  const saveProvider = vi.fn(async () => {});
  const probeProvider = vi.fn();
  const port = {
    protocols: vi.fn(async () => [{ kind: "openai", discovery: "openai", reasoningParams: true }]),
    saveProvider, probeProvider,
  } as unknown as Port;
  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await waitFor(() => expect((screen.getByLabelText("接口协议") as HTMLSelectElement).value).toBe("openai"));
  const advanced = screen.getByText("高级连接选项").closest("details")!;
  expect(advanced.open).toBe(false);
  expect(screen.getByRole("combobox", { name: /^HTTP 连接协议/ }).closest("details")).toBe(advanced);
  await userEvent.click(screen.getByText("高级连接选项"));
  const selector = await screen.findByRole("combobox", { name: /^HTTP 连接协议/ });
  expect(advanced.contains(selector)).toBe(true);
  expect((selector as HTMLSelectElement).value).toBe("auto");
  await userEvent.selectOptions(selector, "http1");
  await userEvent.type(screen.getByLabelText("来源名称"), "fixture");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://fixture.invalid/v1");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "model-a{enter}");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));
  await waitFor(() => expect(saveProvider).toHaveBeenCalledWith(expect.objectContaining({ http1Only: true })));
  expect(probeProvider).not.toHaveBeenCalled();
});
