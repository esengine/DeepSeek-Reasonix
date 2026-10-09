// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderCheck, ProviderEntry } from "../port/port";
import { boot, STORAGE } from "../i18n";
import { EditConn } from "./EditConn";
import { Providers, type Port } from "./Providers";
import { checkFailure } from "./provider_check";

beforeEach(() => {
  const values = new Map<string, string>([[STORAGE, "zh"]]);
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  });
  boot();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const entry: ProviderEntry = {
  name: "relay",
  kind: "openai",
  baseUrl: "https://relay.example.com/v1",
  models: ["relay-chat"],
  default: "relay-chat",
  hasKey: true,
  inUse: true,
  preset: false,
};

const refused: ProviderCheck = {
  ok: false,
  code: "provider.probe.unauthorized",
  httpStatus: 401,
  detail: "Invalid Authentication",
};

function drawDetail(checkProvider: () => Promise<ProviderCheck>) {
  const port = {
    providers: vi.fn(async () => [entry]),
    protocols: vi.fn(async () => []),
    checkProvider: vi.fn(checkProvider),
  } as unknown as Port;
  render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}}
    onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
  return port;
}

it("says why the connection test failed in the window's words, with the status and the endpoint's text", async () => {
  drawDetail(async () => refused);
  await userEvent.click(await screen.findByRole("button", { name: "测试连接" }));
  const found = await screen.findByText(/该 key 未被接受/);
  expect(found.textContent).toContain("HTTP 401");
  expect(found.textContent).toContain("Invalid Authentication");
  expect(screen.getByText("无法连接")).toBeTruthy();
});

it("tells a timeout from an unreachable address", async () => {
  drawDetail(async () => ({ ok: false, code: "provider.probe.timeout" }));
  await userEvent.click(await screen.findByRole("button", { name: "测试连接" }));
  expect(await screen.findByText(/没有响应/)).toBeTruthy();
  expect(screen.queryByText(/HTTP/)).toBeNull();
});

it("still says so when the request never reached a verdict", async () => {
  drawDetail(async () => { throw new Error("kernel went away"); });
  await userEvent.click(await screen.findByRole("button", { name: "测试连接" }));
  expect(await screen.findByText("kernel went away")).toBeTruthy();
  expect(screen.getByText("无法连接")).toBeTruthy();
});

it("keeps the passing result as it was", async () => {
  drawDetail(async () => ({ ok: true, kind: "openai", models: ["relay-chat"], matches: true }));
  await userEvent.click(await screen.findByRole("button", { name: "测试连接" }));
  expect(await screen.findByText(/连上了/)).toBeTruthy();
  expect(screen.getByText(/key 有效，协议也对得上/)).toBeTruthy();
});

it("renders the same failure in English", async () => {
  localStorage.setItem(STORAGE, "en");
  boot();
  drawDetail(async () => refused);
  await userEvent.click(await screen.findByRole("button", { name: "Test it" }));
  expect((await screen.findByText(/That key was refused/)).textContent).toContain("HTTP 401");
});

it("refreshing the catalogue reports a typed failure instead of inventing one", async () => {
  const port = { checkProvider: vi.fn(async () => ({ ok: false, code: "provider.probe.path_not_found", httpStatus: 404 })) } as unknown as Port;
  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  const alert = await screen.findByText(/该路径没有模型清单/);
  expect(alert.textContent).toContain("HTTP 404");
  expect(screen.queryByText("这个端点没报出任何聊天模型")).toBeNull();
  expect(within(document.body).getByDisplayValue("https://relay.example.com/v1")).toBeTruthy();
});

it("refreshing still adds the models a passing check found", async () => {
  const port = { checkProvider: vi.fn(async () => ({ ok: true, models: ["relay-chat", "relay-new"] })) } as unknown as Port;
  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  await waitFor(() => expect(screen.getAllByText("relay-new").length).toBeGreaterThan(0));
});

it("composes the failure line from the code, the status and the endpoint's words", () => {
  expect(checkFailure({ ok: false, code: "provider.probe.upstream_error", params: { status: 502 }, httpStatus: 502 }))
    .toBe("服务商返回错误（HTTP 502），与填写内容无关，请稍后重试 · HTTP 502");
  expect(checkFailure({ ok: false, code: "provider.probe.no_chat_models", params: { count: 3 } })).toContain("3");
});

it("never leaves the cause empty when the kernel sends no code or one this window cannot say", async () => {
  expect(checkFailure({ ok: false })).toBe("检查失败，没有具体原因");
  expect(checkFailure({ ok: false, code: "provider.future_code", httpStatus: 418 })).toBe("检查失败，没有具体原因 · HTTP 418");
  localStorage.setItem(STORAGE, "en");
  boot();
  expect(checkFailure({ ok: false })).toBe("The check failed and gave no reason");
  expect(checkFailure({ ok: false, code: "provider.probe.failed" })).toBe("The check failed and gave no reason");
});

it("shows the generic cause under the heading for a kernel that sent no code", async () => {
  drawDetail(async () => ({ ok: false }));
  await userEvent.click(await screen.findByRole("button", { name: "测试连接" }));
  expect(await screen.findByText("检查失败，没有具体原因")).toBeTruthy();
});
