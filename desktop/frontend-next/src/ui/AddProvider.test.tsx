// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { Port } from "./Providers";
import { AddProvider } from "./AddProvider";

afterEach(cleanup);

it("saves an explicitly configured source without requiring protocol detection", async () => {
  const saveProvider = vi.fn(async () => {});
  const probeProvider = vi.fn();
  const port = {
    protocols: vi.fn(async () => [
      { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true },
      { kind: "anthropic", discovery: "anthropic", serverWebSearch: true, reasoningParams: true },
    ]),
    saveProvider,
    probeProvider,
  } as unknown as Port;

  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await userEvent.type(screen.getByLabelText("来源名称"), "my-relay");
  await userEvent.selectOptions(await screen.findByLabelText("接口协议"), "anthropic");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await userEvent.type(screen.getByLabelText("上下文窗口"), "200000");
  await userEvent.type(screen.getByLabelText("最大输出"), "32000");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "claude-custom{enter}");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));

  await waitFor(() => expect(saveProvider).toHaveBeenCalled());
  expect(probeProvider).not.toHaveBeenCalled();
  expect(saveProvider).toHaveBeenCalledWith(expect.objectContaining({
    name: "my-relay",
    kind: "anthropic",
    models: ["claude-custom"],
    contextWindow: 200000,
    maxOutputTokens: 32000,
  }));
});

it("keeps relay-only controls optional and saves them when disclosed", async () => {
  const saveProvider = vi.fn(async () => {});
  const port = {
    protocols: vi.fn(async () => [
      { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true },
    ]),
    saveProvider,
  } as unknown as Port;

  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await userEvent.type(screen.getByLabelText("来源名称"), "relay");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "model-x{enter}");
  await userEvent.click(screen.getByText("高级连接选项"));
  await userEvent.click(screen.getByRole("switch", { name: "发送思考控制" }));
  await userEvent.type(screen.getByText("额外请求头").parentElement!.querySelector("textarea")!, "X-Title: Reasonix");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));

  await waitFor(() => expect(saveProvider).toHaveBeenCalled());
  expect(saveProvider).toHaveBeenCalledWith(expect.objectContaining({
    reasoningProtocol: "none",
    headers: { "X-Title": "Reasonix" },
  }));
});

const nameCatalog = async () => [{ kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true }];

it("explains an unusable source name next to the field while it is typed and holds the add button", async () => {
  const saveProvider = vi.fn(async () => {});
  const port = { protocols: vi.fn(nameCatalog), saveProvider } as unknown as Port;

  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  const field = screen.getByLabelText("来源名称");
  await userEvent.type(field, "公司中转站");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "m1{enter}");

  expect(field.getAttribute("aria-invalid")).toBe("true");
  expect(screen.getByText(/名称只能用字母、数字、点、连字符和下划线/)).toBeTruthy();
  expect((screen.getByRole("button", { name: "添加来源" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByText("无法连接")).toBeNull();

  await userEvent.clear(field);
  await userEvent.type(field, "company-relay");
  expect(field.getAttribute("aria-invalid")).toBeNull();
  expect((screen.getByRole("button", { name: "添加来源" }) as HTMLButtonElement).disabled).toBe(false);
});

it("shows a coded name refusal from the kernel on the name field, not as a connection failure", async () => {
  const { HttpError } = await import("../port/http_error");
  const saveProvider = vi.fn(async () => {
    throw new HttpError(409, "taken", { code: "provider.name_taken", params: { name: "relay" } });
  });
  const port = { protocols: vi.fn(nameCatalog), saveProvider } as unknown as Port;

  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await userEvent.type(screen.getByLabelText("来源名称"), "relay");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "m1{enter}");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));

  const field = screen.getByLabelText("来源名称");
  await waitFor(() => expect(field.getAttribute("aria-invalid")).toBe("true"));
  expect(screen.getByText("已经有名为「relay」的连接了，换一个名称")).toBeTruthy();
  expect(screen.queryByText("无法连接")).toBeNull();
  expect(screen.queryByText("无法保存")).toBeNull();
});

it("titles a failed save as a save failure, never as a connection failure", async () => {
  const { HttpError } = await import("../port/http_error");
  const saveProvider = vi.fn(async () => {
    throw new HttpError(422, "pick at least one model", { code: "provider.endpoint_required" });
  });
  const port = { protocols: vi.fn(nameCatalog), saveProvider } as unknown as Port;

  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await userEvent.type(screen.getByLabelText("来源名称"), "relay");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "m1{enter}");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));

  expect(await screen.findByText("无法保存")).toBeTruthy();
  expect(screen.queryByText("无法连接")).toBeNull();
});

it("ties the name rule to the field and the button, and waits out IME composition", async () => {
  const { fireEvent } = await import("@testing-library/react");
  const port = { protocols: vi.fn(nameCatalog), saveProvider: vi.fn() } as unknown as Port;
  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  const field = screen.getByLabelText("来源名称");
  expect(field.getAttribute("aria-describedby")).toBe("addp-name-rule");

  fireEvent.compositionStart(field);
  fireEvent.change(field, { target: { value: "gong'si" } });
  expect(field.getAttribute("aria-invalid")).toBeNull();
  fireEvent.compositionEnd(field);
  expect(field.getAttribute("aria-invalid")).toBe("true");
  expect(screen.getByRole("button", { name: "添加来源" }).getAttribute("aria-describedby")).toBe("addp-name-rule");
});

it("keeps Add disabled while an IME name is still being composed", async () => {
  const { fireEvent } = await import("@testing-library/react");
  const saveProvider = vi.fn();
  const port = { protocols: vi.fn(nameCatalog), saveProvider } as unknown as Port;
  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);

  const name = screen.getByLabelText("来源名称");
  fireEvent.change(name, { target: { value: "company-relay" } });
  fireEvent.change(screen.getByLabelText("接口地址"), { target: { value: "https://relay.example/v1" } });
  const model = screen.getByRole("searchbox", { name: "搜索或添加模型" });
  fireEvent.change(model, { target: { value: "m1" } });
  fireEvent.keyDown(model, { key: "Enter", code: "Enter", charCode: 13 });
  const add = screen.getByRole("button", { name: "添加来源" }) as HTMLButtonElement;
  await waitFor(() => expect(add.disabled).toBe(false));

  fireEvent.compositionStart(name);
  fireEvent.change(name, { target: { value: "公司中转站" } });
  expect(add.disabled).toBe(true);
  fireEvent.click(add);
  expect(saveProvider).not.toHaveBeenCalled();

  fireEvent.compositionEnd(name);
  expect(add.disabled).toBe(true);
  fireEvent.change(name, { target: { value: "company-relay" } });
  expect(add.disabled).toBe(false);
});

it("normalizes spaces around an ASCII name and refuses a blank one", async () => {
  const { fireEvent } = await import("@testing-library/react");
  const saveProvider = vi.fn(async () => {});
  const port = { protocols: vi.fn(nameCatalog), saveProvider } as unknown as Port;
  render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);

  const name = screen.getByLabelText("来源名称");
  const add = screen.getByRole("button", { name: "添加来源" }) as HTMLButtonElement;
  expect(add.disabled).toBe(true);
  fireEvent.change(name, { target: { value: "   " } });
  expect(add.disabled).toBe(true);

  fireEvent.change(name, { target: { value: " company-relay.1_x " } });
  fireEvent.change(screen.getByLabelText("接口地址"), { target: { value: "https://relay.example/v1" } });
  const model = screen.getByRole("searchbox", { name: "搜索或添加模型" });
  fireEvent.change(model, { target: { value: "m1" } });
  fireEvent.keyDown(model, { key: "Enter", code: "Enter", charCode: 13 });
  await waitFor(() => expect(add.disabled).toBe(false));

  fireEvent.click(add);
  await waitFor(() => expect(saveProvider).toHaveBeenCalledWith(expect.objectContaining({
    name: "company-relay.1_x",
  })));
});
