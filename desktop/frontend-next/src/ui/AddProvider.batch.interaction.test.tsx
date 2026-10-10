// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SsePort } from "../port/sse";
import type { ProviderModelCheck, ProviderModelCheckRequest } from "../port/port";
import { AddProvider } from "./AddProvider";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const names = ["m1", "m2", "m3", "m4", "m5"];
const all = () => screen.getByRole("button", { name: /^测试已启用模型/ }) as HTMLButtonElement;
const summary = () => document.querySelector(".msummary")?.textContent;
const row = (model: string) => screen.getByText(model).closest(".mline") as HTMLElement;

async function draft({ strict = false, noProxy = false, probe = true } = {}) {
  const pending: Array<{ model: string; settle: (result: ProviderModelCheck | Error) => void }> = [];
  const calls: ProviderModelCheckRequest[] = [];
  let peak = 0;
  let live = 0;
  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    if (url.endsWith("/providers/protocols")) return Response.json([
      { kind: "openai", discovery: "openai", reasoningParams: true },
      { kind: "anthropic", discovery: "anthropic", reasoningParams: true },
    ]);
    if (url.endsWith("/providers/probe")) return Response.json({
      kind: "openai", models: names, default: names[0], efforts: [], effort: "",
      vision: ["m1"], authHeader: true, noProxy,
    });
    if (url.endsWith("/providers/check/model")) {
      const req = JSON.parse(init!.body as string) as ProviderModelCheckRequest;
      calls.push(req);
      peak = Math.max(peak, ++live);
      return new Promise((resolve, reject) => pending.push({ model: req.model, settle: (result) => {
        live--;
        if (result instanceof Error) reject(result); else resolve(Response.json(result));
      } }));
    }
    if (url.endsWith("/providers") && init?.method === "POST") return new Response(null, { status: 204 });
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  const onDone = vi.fn();
  const onCancel = vi.fn();
  const port = new SsePort("/batch-kernel");
  const form = (p = port) => <AddProvider port={p} taken={[]} known={[]} onDone={onDone} onCancel={onCancel} />;
  const view = render(strict ? <StrictMode>{form()}</StrictMode> : form());
  const user = userEvent.setup();
  await screen.findByRole("option", { name: "Anthropic 兼容" });
  await user.type(screen.getByLabelText("来源名称"), "relay-draft");
  await user.type(screen.getByLabelText("接口地址"), " https://relay.example/v1 ");
  await user.type(screen.getByLabelText("API Key"), " draft-key ");
  if (probe) {
    await user.click(screen.getByRole("button", { name: "验证连接并读取" }));
    await screen.findByText("连接可用 · 找到 5 个模型");
  }
  const finish = async (model: string, result: ProviderModelCheck | Error = { model, status: "available" }) => {
    const index = pending.findIndex((p) => p.model === model);
    expect(index).toBeGreaterThanOrEqual(0);
    const [request] = pending.splice(index, 1);
    await act(async () => request.settle(result));
  };
  const add = async (model: string) => {
    const search = screen.getByRole("searchbox", { name: "搜索或添加模型" });
    await user.clear(search);
    await user.type(search, `${model}{enter}`);
    await user.clear(search);
  };
  const saveCalls = () => fetchMock.mock.calls.filter(([url, init]) => url.endsWith("/providers") && init?.method === "POST");
  return { user, calls, pending, finish, add, peak: () => peak, saveCalls, onDone, onCancel, view, form };
}

for (const strict of [false, true]) it(`sends at most two requests for enabled discovered and manual models (StrictMode=${strict})`, async () => {
  const g = await draft({ strict });
  await g.user.click(screen.getByRole("checkbox", { name: "选用 m5" }));
  await g.add("manual/Exact-ID");
  await g.user.selectOptions(screen.getByLabelText("接口协议"), "anthropic");
  const liveRegion = document.querySelector(".msummary[role=status]");
  await g.user.click(all());
  expect(all().getAttribute("aria-busy")).toBe("true");
  expect(summary()).toBe("2 个验证中 · 3 个未验证");
  expect(g.calls.map((r) => r.model)).toEqual(["m1", "m2"]);
  await g.user.click(all());
  expect(g.calls).toHaveLength(2);
  for (const model of ["m1", "m2", "m3", "m4", "manual/Exact-ID"]) await g.finish(model);
  await waitFor(() => expect(all().disabled).toBe(false));
  expect(g.calls.map((r) => r.model)).toEqual(["m1", "m2", "m3", "m4", "manual/Exact-ID"]);
  expect(g.peak()).toBe(2);
  for (const req of g.calls) expect(req).toMatchObject({ baseUrl: "https://relay.example/v1", apiKey: "draft-key", kind: "anthropic", authHeader: true, noProxy: false });
  expect(summary()).toBe("5 个可用");
  expect(document.querySelector(".msummary[role=status]")).toBe(liveRegion);
  expect(within(row("manual/Exact-ID")).getByText("用户添加")).toBeTruthy();
  expect(g.saveCalls()).toHaveLength(0);
  await g.user.click(screen.getByRole("button", { name: "添加来源" }));
  await waitFor(() => expect(g.onDone).toHaveBeenCalledOnce());
  expect(JSON.parse(g.saveCalls()[0][1]!.body as string)).toMatchObject({
    name: "relay-draft", kind: "anthropic", models: ["m1", "m2", "m3", "m4", "manual/Exact-ID"], default: "m1", vision: ["m1"],
  });
});

it("keeps per-model HTTP evidence and continues after partial failures without changing selection", async () => {
  const g = await draft();
  await g.user.click(all());
  await g.finish("m1");
  await g.finish("m2", { model: "m2", status: "unavailable", reason: "not_found", httpStatus: 404, detail: "no such model" });
  await g.finish("m3", new Error("connection closed"));
  await g.finish("m4", { model: "m4", status: "unknown", reason: "rejected", httpStatus: 400, detail: "tools unsupported" });
  await g.finish("m5");
  expect(summary()).toBe("2 个可用 · 1 个不可用 · 2 个无法确定");
  expect(row("m2").textContent).toContain("HTTP 404");
  expect(row("m2").textContent).toContain("no such model");
  expect(row("m4").textContent).toContain("HTTP 400");
  expect(row("m4").textContent).toContain("tools unsupported");
  for (const model of names) expect(within(row(model)).getByRole("checkbox").getAttribute("aria-checked")).toBe("true");
  expect(document.querySelectorAll(".mevidence[aria-live]")).toHaveLength(0);
  expect(document.querySelectorAll(".msummary[role=status]")).toHaveLength(1);
  await g.user.click(all());
  await g.finish("m2", new Error("connection closed"));
  expect(row("m2").querySelector(".mhttp")).toBeNull();
  expect(row("m2").querySelector(".mdetail")).toBeNull();
  expect(row("m2").querySelector(".mevidence i")?.getAttribute("data-state")).toBe("unknown");
});

it.each([false, true])("uses the displayed proxy route for the whole batch (probe bypass=%s)", async (noProxy) => {
  const g = await draft({ noProxy });
  await g.user.click(screen.getByText("高级连接选项"));
  if (!noProxy) await g.user.click(screen.getByRole("switch", { name: "绕过系统代理" }));
  await g.user.click(all());
  for (const model of names) await g.finish(model);
  expect(g.calls.every((r) => r.noProxy && r.authHeader)).toBe(true);
});

it("holds conflicting actions during a batch and restores them when it settles", async () => {
  const g = await draft();
  await g.user.click(all());
  const controls = [all(), screen.getByRole("button", { name: "添加来源" }), screen.getByRole("button", { name: "验证连接并读取" }), screen.getByRole("button", { name: "验证模型 m3" }), ...screen.getAllByRole("button", { name: "取消" })];
  for (const control of controls) {
    expect((control as HTMLButtonElement).disabled).toBe(true);
    await g.user.click(control);
  }
  for (const label of ["来源名称", "接口地址", "API Key", "接口协议"]) expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(true);
  expect(g.calls).toHaveLength(2);
  expect(g.saveCalls()).toHaveLength(0);
  expect(g.onCancel).not.toHaveBeenCalled();
  for (const model of names) await g.finish(model);
  for (const control of controls) expect((control as HTMLButtonElement).disabled).toBe(false);
});

it.each(["limit", "selection", "manual model", "extra body"])("stops queued checks and discards old answers after changing %s", async (change) => {
  const g = await draft();
  await g.user.click(all());
  if (change === "limit") await g.user.type(screen.getByLabelText("上下文窗口"), "1");
  if (change === "selection") await g.user.click(screen.getByRole("checkbox", { name: "选用 m5" }));
  if (change === "manual model") await g.add("new/model");
  if (change === "extra body") {
    await g.user.click(screen.getByText("高级连接选项"));
    await g.user.type(screen.getByRole("textbox", { name: /^额外请求体/ }), " ");
  }
  expect(screen.getByText("已停止启动新的验证：草稿已改动")).toBeTruthy();
  expect(document.querySelectorAll('.mevidence i[data-state="checking"]')).toHaveLength(0);
  await g.finish("m1");
  await g.finish("m2");
  expect(g.calls).toHaveLength(2);
  expect(document.querySelectorAll('.mevidence i[data-state="available"]')).toHaveLength(0);
  expect(all().disabled).toBe(false);
  await g.user.click(all());
  expect(screen.queryByText("已停止启动新的验证：草稿已改动")).toBeNull();
  expect(g.calls).toHaveLength(4);
});

it("a stopped batch cannot overwrite a newer run, and a completed receipt survives cancellation", async () => {
  const g = await draft();
  await g.user.click(all());
  await g.finish("m1");
  await g.user.type(screen.getByLabelText("上下文窗口"), "1");
  expect(summary()).toBe("1 个可用 · 4 个未验证");
  await g.user.click(all());
  await g.finish("m2", { model: "m2", status: "unavailable", reason: "not_found" });
  expect(all().getAttribute("aria-busy")).toBe("true");
  expect(row("m2").querySelector(".mevidence i")?.getAttribute("data-state")).toBe("checking");
  await g.finish("m3");
  expect(g.calls.map((r) => r.model)).toEqual(["m1", "m2", "m3", "m1", "m2"]);
  await g.finish("m1");
  await g.finish("m2");
  for (const model of ["m3", "m4", "m5"]) await g.finish(model);
  expect(summary()).toBe("5 个可用");
});

it.each(["unmount", "port change"])("stops queued checks on %s", async (change) => {
  const g = await draft();
  await g.user.click(all());
  if (change === "unmount") g.view.unmount(); else g.view.rerender(g.form(new SsePort("/other-kernel")));
  await g.finish("m1");
  await g.finish("m2");
  expect(g.calls).toHaveLength(2);
  if (change === "port change") {
    expect(all().disabled).toBe(false);
    expect(document.querySelectorAll('.mevidence i[data-state="checking"]')).toHaveLength(0);
  }
});

it("requires an address and an enabled model, and excludes a concurrent single check", async () => {
  const g = await draft({ probe: false });
  expect(all().disabled).toBe(true);
  expect(screen.getByText(/每个已勾选的模型各发送一次小请求/)).toBeTruthy();
  await g.add("manual/model");
  await g.user.clear(screen.getByLabelText("接口地址"));
  expect(all().disabled).toBe(true);
  await g.user.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await g.user.click(screen.getByRole("button", { name: "验证模型 manual/model" }));
  expect(all().disabled).toBe(true);
  await g.user.click(all());
  expect(g.calls).toHaveLength(1);
  await g.finish("manual/model");
  expect(all().disabled).toBe(false);
  await g.user.click(screen.getByRole("checkbox", { name: "选用 manual/model" }));
  expect(all().disabled).toBe(true);
});
