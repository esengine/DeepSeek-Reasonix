// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddProvider } from "./AddProvider";
import { SsePort } from "../port/sse";
import type { ProviderProbe } from "../port/port";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const first = "https://relay-a.example/v1";
const second = "https://relay-b.example/v1";
const model = "shared-model";
const detected: ProviderProbe = {
  kind: "anthropic", baseUrl: first, kinds: ["anthropic"], authHeader: true,
  models: [model], default: model, efforts: [], effort: "", vision: [model],
  ambiguous: false, noProxy: true,
};
const endpoint = () => screen.getByLabelText<HTMLInputElement>("接口地址");
const bypass = () => screen.getByRole<HTMLButtonElement>("switch", { name: "绕过系统代理" });
const change = (value: string) => fireEvent.change(endpoint(), { target: { value } });

async function fixture(strict: boolean, manualBypass = false) {
  let reply = detected;
  let refused = false;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/kernel/providers/protocols") return Response.json([
      { kind: "anthropic", discovery: "anthropic", serverWebSearch: true, reasoningParams: true },
    ]);
    if (url === "/kernel/providers/probe") return refused
      ? Response.json({ code: "provider.probe.unauthorized", error: "new endpoint refusal" }, { status: 401 })
      : Response.json(reply);
    if (url === "/kernel/providers/check/model") return Response.json({ model, status: "available" });
    if (url === "/kernel/providers" && init?.method === "POST") return new Response(null, { status: 204 });
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  const done = vi.fn();
  const form = <AddProvider port={new SsePort("/kernel")} taken={[]} known={[]} onDone={done} onCancel={() => {}} />;
  render(strict ? <StrictMode>{form}</StrictMode> : form);
  await waitFor(() => expect(screen.getByLabelText<HTMLSelectElement>("接口协议").value).toBe("anthropic"));
  const user = userEvent.setup();
  fireEvent.change(screen.getByLabelText("来源名称"), { target: { value: "relay-draft" } });
  change(first);
  fireEvent.change(document.querySelector<HTMLInputElement>('[data-value="credential"]')!, { target: { value: "fixture-key" } });
  await user.click(screen.getByText("高级连接选项"));
  if (manualBypass) await user.click(bypass());
  const probe = async () => {
    await user.click(screen.getByRole("button", { name: "验证连接并读取" }));
    await waitFor(() => expect(screen.getByRole<HTMLButtonElement>("button", { name: "验证连接并读取" }).disabled).toBe(false));
  };
  await probe();
  expect(screen.getByText("连接可用 · 找到 1 个模型")).toBeTruthy();
  expect(bypass().getAttribute("aria-checked")).toBe("true");
  expect(bypass().disabled).toBe(true);
  expect(document.querySelector(".mline .vtag")?.textContent).toBe("读图");
  const requests = (path: string) => fetcher.mock.calls.filter(([url, init]) => url === path && init?.method === "POST")
    .map(([, init]) => JSON.parse(init!.body as string));
  const save = async () => {
    await user.click(screen.getByRole("button", { name: "添加来源" }));
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1));
    expect(requests("/kernel/providers")).toHaveLength(1);
    return requests("/kernel/providers")[0];
  };
  return { user, probe, requests, save, setReply: (got: ProviderProbe) => { reply = got; }, refuse: () => { refused = true; } };
}

for (const strict of [false, true]) describe(`probe endpoint ownership with StrictMode=${strict}`, () => {
  it("checks and saves the edited endpoint without the old endpoint's flags", async () => {
    const f = await fixture(strict);
    await f.user.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "manual-id{enter}");
    fireEvent.change(screen.getByLabelText("上下文窗口"), { target: { value: "200000" } });
    change(second);
    await f.user.click(screen.getByRole("button", { name: `验证模型 ${model}` }));
    await waitFor(() => expect(screen.getByRole("checkbox", { name: `选用 ${model}` }).closest(".mline")?.querySelector(".mevidence")?.textContent).toContain("已验证可用"));
    const saved = await f.save();
    expect(saved).toMatchObject({
      name: "relay-draft", kind: "anthropic", baseUrl: second, apiKey: "fixture-key",
      models: [model, "manual-id"], default: model, contextWindow: 200000,
      authHeader: false, noProxy: false, vision: [],
    });
    expect(f.requests("/kernel/providers/check/model")).toEqual([{
      model, kind: "anthropic", baseUrl: second, apiKey: "fixture-key", authHeader: false, noProxy: false,
    }]);
    expect(f.requests("/kernel/providers/probe")).toEqual([{ baseUrl: first, apiKey: "fixture-key" }]);
  });

  it("releases the detected bypass and image/connection hints when the address changes", async () => {
    const f = await fixture(strict);
    change(second);
    expect(bypass().disabled).toBe(false);
    expect(bypass().getAttribute("aria-checked")).toBe("false");
    expect(screen.queryByText("连接可用 · 找到 1 个模型")).toBeNull();
    expect(screen.queryByText("该来源通过代理无法连接、直连可用，已记录为「此来源不使用代理」。")).toBeNull();
    expect(document.querySelector(".mline .vtag")).toBeNull();
    expect(screen.getByRole("checkbox", { name: `选用 ${model}` }).getAttribute("aria-checked")).toBe("true");
    await f.user.click(bypass());
    expect((await f.save()).noProxy).toBe(true);
  });

  it("cannot reuse the old success after a failed probe of the edited endpoint", async () => {
    const f = await fixture(strict);
    change(second);
    f.refuse();
    await f.probe();
    await screen.findByText("无法连接");
    const saved = await f.save();
    expect(saved).toMatchObject({ baseUrl: second, authHeader: false, noProxy: false, vision: [] });
    expect(screen.queryByText("连接可用 · 找到 1 个模型")).toBeNull();
    expect(f.requests("/kernel/providers/probe").map((r) => r.baseUrl)).toEqual([first, second]);
  });

  it("preserves findings for whitespace around the same effective endpoint", async () => {
    const f = await fixture(strict);
    change(`  ${first}  `);
    expect(bypass().disabled).toBe(true);
    expect(screen.getByText("连接可用 · 找到 1 个模型")).toBeTruthy();
    expect(document.querySelector(".mline .vtag")?.textContent).toBe("读图");
    expect(await f.save()).toMatchObject({ baseUrl: first, authHeader: true, noProxy: true, vision: [model] });
  });

  it("uses fresh findings after a successful probe of the edited endpoint", async () => {
    const f = await fixture(strict);
    change(second);
    f.setReply({ ...detected, baseUrl: second, authHeader: false, noProxy: false, vision: [] });
    await f.probe();
    expect(screen.getByText("连接可用 · 找到 1 个模型")).toBeTruthy();
    expect(bypass().disabled).toBe(false);
    expect(bypass().getAttribute("aria-checked")).toBe("false");
    expect(await f.save()).toMatchObject({ baseUrl: second, authHeader: false, noProxy: false, vision: [] });
  });

  it("keeps a manually chosen bypass while releasing the old automatic lock", async () => {
    const f = await fixture(strict, true);
    change(second);
    expect(bypass().disabled).toBe(false);
    expect(bypass().getAttribute("aria-checked")).toBe("true");
    expect(await f.save()).toMatchObject({ baseUrl: second, authHeader: false, noProxy: true, vision: [] });
  });

  it("requires fresh detection even when the user returns to the old address", async () => {
    const f = await fixture(strict);
    change(second);
    change(first);
    const saved = await f.save();
    expect(saved).toMatchObject({ baseUrl: first, authHeader: false, noProxy: false, vision: [] });
    expect(screen.queryByText("连接可用 · 找到 1 个模型")).toBeNull();
    expect(f.requests("/kernel/providers/probe")).toHaveLength(1);
  });
});
