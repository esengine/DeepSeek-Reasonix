// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SsePort } from "../port/sse";
import type { ProviderModelCheck } from "../port/port";
import { AddProvider } from "./AddProvider";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const model = "Hidden-Preview/Exact-ID";
const baseUrl = "https://relay.example/v1";
const apiKey = "draft-test-key";

async function draft(strict: boolean, probeNoProxy?: boolean) {
  const pending: ((response: Response) => void)[] = [];
  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    if (url === "/test-kernel/providers/protocols") return Response.json([
      { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true },
      { kind: "anthropic", discovery: "anthropic", serverWebSearch: true, reasoningParams: true },
    ]);
    if (url === "/test-kernel/providers/probe") return Response.json({
      kind: "openai", kinds: ["openai"], baseUrl, authHeader: true,
      models: [model], default: model, efforts: [], effort: "", vision: [],
      ambiguous: false, noProxy: probeNoProxy,
    });
    if (url === "/test-kernel/providers/check/model") return new Promise((resolve) => pending.push(resolve));
    if (url === "/test-kernel/providers" && init?.method === "POST") return new Response(null, { status: 204 });
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  const onDone = vi.fn();
  const onCancel = vi.fn();
  const form = <AddProvider port={new SsePort("/test-kernel")} taken={[]} known={[]} onDone={onDone} onCancel={onCancel} />;
  render(strict ? <StrictMode>{form}</StrictMode> : form);
  const user = userEvent.setup();
  await screen.findByRole("option", { name: "Anthropic 兼容" });
  await user.type(screen.getByLabelText("来源名称"), "relay-draft");
  await user.type(screen.getByLabelText("接口地址"), ` ${baseUrl} `);
  await user.type(screen.getByLabelText("API Key"), ` ${apiKey} `);
  if (probeNoProxy === undefined) {
    await user.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), `${model}{enter}`);
  } else {
    await user.click(screen.getByRole("button", { name: "验证连接并读取" }));
    await screen.findByText("连接可用 · 找到 1 个模型");
  }
  await user.selectOptions(screen.getByLabelText("接口协议"), "anthropic");
  await user.click(screen.getByText("高级连接选项"));
  const row = screen.getByText(model).closest(".mline") as HTMLElement;
  const bypass = screen.getByRole("switch", { name: "绕过系统代理" }) as HTMLButtonElement;
  const check = within(row).getByRole("button", { name: `验证模型 ${model}` }) as HTMLButtonElement;
  const save = screen.getByRole("button", { name: "添加来源" }) as HTMLButtonElement;
  const requests = (path: string) => fetchMock.mock.calls.filter(([url]) => url === `/test-kernel${path}`);
  const body = (path: string, index = 0) => JSON.parse(requests(path)[index][1]!.body as string);
  const finish = async (result: ProviderModelCheck) => {
    const resolve = pending.shift()!;
    await act(async () => resolve(Response.json(result)));
    await waitFor(() => expect(check.disabled).toBe(false));
  };
  return { user, row, bypass, check, save, requests, body, finish, onDone, onCancel };
}

for (const strict of [false, true]) describe(`proxy setting with StrictMode=${strict}`, () => {
  it.each([
    { label: "manual off", manual: false, probe: undefined, expected: false },
    { label: "manual on", manual: true, probe: undefined, expected: true },
    { label: "probe off", manual: false, probe: false, expected: false },
    { label: "manual override of probe off", manual: true, probe: false, expected: true },
    { label: "probe requires bypass", manual: false, probe: true, expected: true },
  ])("checks and saves the displayed route: $label", async ({ manual, probe, expected }) => {
    const form = await draft(strict, probe);
    const { user, row, bypass, check, save, requests, body, finish, onDone, onCancel } = form;
    expect(bypass.disabled).toBe(probe === true);
    if (manual) await user.click(bypass);
    expect(bypass.getAttribute("aria-checked")).toBe(String(expected));
    if (probe === true) {
      await user.click(bypass);
      expect(bypass.getAttribute("aria-checked")).toBe("true");
    }
    await user.click(check);
    expect(check.disabled).toBe(true);
    expect(bypass.disabled).toBe(true);
    expect(save.disabled).toBe(true);
    for (const label of ["来源名称", "接口地址", "接口协议", "API Key"]) {
      expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(true);
    }
    for (const cancel of screen.getAllByRole("button", { name: "取消" })) {
      expect((cancel as HTMLButtonElement).disabled).toBe(true);
    }
    expect(row.querySelector(".mevidence i")?.getAttribute("data-state")).toBe("checking");
    await user.click(bypass);
    await user.click(check);
    expect(requests("/providers/check/model")).toHaveLength(1);
    expect(requests("/providers")).toHaveLength(0);
    await finish({ model, status: "available" });
    expect(body("/providers/check/model")).toEqual({
      model, baseUrl, apiKey, kind: "anthropic", authHeader: probe !== undefined, noProxy: expected,
    });
    expect(requests("/providers/check/model")[0][1]).toMatchObject({
      method: "POST", credentials: "same-origin", headers: { "content-type": "application/json" },
    });
    expect(bypass.disabled).toBe(probe === true);
    expect(row.querySelector(".mevidence i")?.getAttribute("data-state")).toBe("available");
    expect(within(row).getByRole("checkbox").getAttribute("aria-checked")).toBe("true");
    await user.click(save);
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(requests("/providers")).toHaveLength(1);
    expect(body("/providers")).toMatchObject({
      name: "relay-draft", baseUrl, apiKey, kind: "anthropic", models: [model], default: model,
      authHeader: probe !== undefined, noProxy: expected,
    });
    expect(onCancel).not.toHaveBeenCalled();
  });

  it.each<ProviderModelCheck>([
    { model, status: "available" },
    { model, status: "unavailable", reason: "not_found", httpStatus: 404, detail: "Model was not found" },
    { model, status: "unknown", reason: "network", httpStatus: 502, detail: "Connection unavailable" },
  ])("invalidates a settled $status receipt when the route changes in either direction", async (result) => {
    const { user, row, bypass, check, save, body, finish, onDone } = await draft(strict);
    await user.click(check);
    await finish(result);
    expect(row.querySelector(".mevidence i")?.getAttribute("data-state")).toBe(result.status);
    expect(body("/providers/check/model").noProxy).toBe(false);
    for (const [index, expected] of [true, false].entries()) {
      await user.click(bypass);
      expect(bypass.getAttribute("aria-checked")).toBe(String(expected));
      expect(row.querySelector(".mevidence i")?.getAttribute("data-state")).toBe("unverified");
      expect(within(row).getByText("用户添加")).toBeTruthy();
      expect(within(row).getByText("待验证")).toBeTruthy();
      expect(row.querySelector(".mhttp")).toBeNull();
      expect(row.querySelector(".mdetail")).toBeNull();
      expect(within(row).getByRole("checkbox").getAttribute("aria-checked")).toBe("true");
      await user.click(check);
      await finish(result);
      expect(body("/providers/check/model", index + 1)).toMatchObject({
        model, baseUrl, apiKey, kind: "anthropic", authHeader: false, noProxy: expected,
      });
    }
    await user.click(save);
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(body("/providers")).toMatchObject({ models: [model], noProxy: false });
  });
});
