// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SsePort } from "../port/sse";
import type { ProviderEntry } from "../port/port";
import { AddProvider } from "./AddProvider";
import { EditConn } from "./EditConn";
import type { Port } from "./Providers";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const entry: ProviderEntry = {
  name: "relay", kind: "openai", baseUrl: "https://relay.example.com/v1",
  models: ["alpha", "beta", "gamma"], default: "alpha", hasKey: true, inUse: false, preset: false,
};

function draw(port: Partial<Port>) {
  return render(<EditConn entry={entry} port={port as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
}
const list = () => document.querySelector(".mlist") as HTMLElement;
const verify = (m: string) => userEvent.click(screen.getByRole("button", { name: `验证模型 ${m}` }));
const summary = () => document.querySelector(".msummary");
const said = () => summary()?.textContent ?? "";

it("shows a refresh failure inside the model section, under its button", async () => {
  draw({ checkProvider: vi.fn(async () => ({ ok: false, code: "provider.probe.path_not_found", httpStatus: 404 })) });
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  const failure = await screen.findByText("读取模型列表失败");
  expect(list().contains(failure)).toBe(true);
  expect(failure.closest(".find")?.getAttribute("role")).toBe("alert");
  expect(failure.closest(".find")?.textContent).toContain("HTTP 404");
});

it("says so when a refresh finds nothing new, instead of staying silent", async () => {
  draw({ checkProvider: vi.fn(async () => ({ ok: true, models: ["alpha", "beta", "gamma"] })) });
  expect(screen.queryByText("没有发现新模型")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  const note = await screen.findByText("没有发现新模型");
  expect(list().contains(note)).toBe(true);
});

it("reports what a refresh added and what it kept, in the model section", async () => {
  draw({ checkProvider: vi.fn(async () => ({ ok: true, models: ["alpha", "delta"] })) });
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  const note = await screen.findByText(/发现 1 个新模型/);
  expect(list().contains(note)).toBe(true);
  expect(note.textContent).toContain("2 个已配置模型本次未返回");
});

it("clears the refresh result once the address changes, and a later failure replaces a success", async () => {
  const checkProvider = vi.fn()
    .mockResolvedValueOnce({ ok: true, models: ["alpha", "beta", "gamma"] })
    .mockResolvedValueOnce({ ok: false, code: "provider.probe.path_not_found", httpStatus: 404 });
  draw({ checkProvider });
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  await screen.findByText("没有发现新模型");
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));
  await screen.findByText("读取模型列表失败");
  expect(screen.queryByText("没有发现新模型")).toBeNull();
});

it("sums up the verified rows in one line and leaves unticked rows out", async () => {
  const checkProviderModel = vi.fn(async (r: { model: string }) =>
    r.model === "alpha" ? { model: r.model, status: "available" as const } : { model: r.model, status: "unavailable" as const, reason: "not_found" as const, httpStatus: 404 });
  draw({ checkProviderModel });
  expect(said()).toBe("");
  await userEvent.click(screen.getByRole("checkbox", { name: "选用 gamma" }));
  await verify("alpha");
  await waitFor(() => expect(summary()?.textContent).toContain("1 个可用"));
  await verify("beta");
  await waitFor(() => expect(summary()?.textContent).toBe("1 个可用 · 1 个不可用"));
  expect(summary()?.getAttribute("role")).toBe("status");
  await userEvent.click(screen.getByRole("checkbox", { name: "选用 gamma" }));
  expect(summary()?.textContent).toBe("1 个可用 · 1 个不可用 · 1 个未验证");
});

it("keeps each row's own failure with status and the endpoint's words under the summary", async () => {
  const checkProviderModel = vi.fn(async () => ({ model: "alpha", status: "unavailable" as const, reason: "rejected" as const, httpStatus: 400, detail: "model is not served here" }));
  draw({ checkProviderModel });
  await verify("alpha");
  const row = (await screen.findByText("HTTP 400")).closest(".mline") as HTMLElement;
  expect(row.textContent).toContain("model is not served here");
  expect(summary()?.textContent).toBe("1 个不可用 · 2 个未验证");
});

it("drops the summary when the address changes, as the row results go", async () => {
  draw({ checkProviderModel: vi.fn(async () => ({ model: "alpha", status: "available" as const })) });
  await verify("alpha");
  await waitFor(() => expect(said()).not.toBe(""));
  await userEvent.type(screen.getByLabelText("接口地址"), "x");
  expect(said()).toBe("");
});

it("shows a connection failure in the add form under the button that ran it, a save failure at the bottom", async () => {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    if (url === "/k/providers/protocols") return Response.json([{ kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true }]);
    if (url === "/k/providers/probe") return Response.json({ code: "provider.probe.path_not_found", error: "x" }, { status: 404 });
    if (url === "/k/providers" && init?.method === "POST") return Response.json({ code: "provider.invalid", error: "bad" }, { status: 400 });
    throw new Error(url);
  });
  vi.stubGlobal("fetch", fetchMock);
  render(<AddProvider port={new SsePort("/k")} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await screen.findByLabelText("接口协议");
  await userEvent.type(screen.getByLabelText("来源名称"), "relay-new");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example.com/v1");
  const button = screen.getByRole("button", { name: "验证连接并读取" });
  await userEvent.click(button);
  const failure = await screen.findByText("无法连接");
  expect(failure.closest(".addp-section")).toBe(button.closest(".addp-section"));
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "m1{enter}");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));
  const saveFailure = await screen.findByText("无法保存");
  expect(saveFailure.closest(".addp-section")).toBeNull();
  expect(within(button.closest(".addp-section") as HTMLElement).queryByText("无法连接")).toBeNull();
});
