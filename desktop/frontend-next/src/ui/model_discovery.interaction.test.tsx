// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderEntry } from "../port/port";
import { EditConn } from "./EditConn";
import type { Port } from "./Providers";

afterEach(cleanup);

const entry: ProviderEntry = {
  name: "relay", kind: "openai", baseUrl: "https://relay.example.com/v1",
  models: ["alpha", "beta"], default: "alpha", hasKey: true, inUse: false, preset: false,
};

function Host({ port }: { port: Partial<Port> }) {
  const [busy, setBusy] = useState("");
  return <EditConn entry={entry} port={port as Port} busy={busy} setBusy={setBusy} onDone={() => {}} onRevert={() => {}} />;
}
function draw(port: Partial<Port>) {
  return render(<Host port={port} />);
}
const header = () => screen.getByRole("button", { name: "从服务商读取可用模型" });
const inline = () => screen.getByRole("button", { name: "没找到？从服务商读取可用模型" });
const search = () => screen.getByRole("searchbox", { name: "搜索或添加模型" });

it("names the header action by what it does", () => {
  draw({});
  expect(header()).toBeTruthy();
  expect(screen.queryByRole("button", { name: "刷新模型目录" })).toBeNull();
});

it("offers the same read beside the add-model field and merges into the list keeping ticks", async () => {
  const checkProvider = vi.fn(async () => ({ ok: true, models: ["alpha", "beta", "gamma"] }));
  draw({ checkProvider });
  await userEvent.click(screen.getByRole("checkbox", { name: "选用 beta" }));
  await userEvent.click(inline());
  expect(checkProvider).toHaveBeenCalledWith("relay");
  await screen.findByText("gamma");
  expect(screen.getByRole("checkbox", { name: "选用 alpha" }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole("checkbox", { name: "选用 beta" }).getAttribute("aria-checked")).toBe("false");
  expect(screen.getByRole("checkbox", { name: "选用 gamma" }).getAttribute("aria-checked")).toBe("false");
  expect((await screen.findByText(/发现 1 个新模型/))).toBeTruthy();
});

it("shows the typed failure next to the add field and points to typing an id", async () => {
  draw({ checkProvider: vi.fn(async () => ({ ok: false, code: "provider.probe.path_not_found", httpStatus: 404 })) });
  await userEvent.click(inline());
  const failure = await screen.findByText("读取模型列表失败");
  const box = failure.closest(".find") as HTMLElement;
  expect(box.getAttribute("role")).toBe("alert");
  expect(box.textContent).toContain("HTTP 404");
  expect(box.textContent).toContain("可以直接输入完整模型 ID 添加");
  expect(search().closest(".mlist")!.contains(box)).toBe(true);
});

it("explains an empty answer in place with the typed code", async () => {
  draw({ checkProvider: vi.fn(async () => ({ ok: true, models: [] })) });
  await userEvent.click(inline());
  const failure = await screen.findByText("读取模型列表失败");
  expect(failure.closest(".find")!.textContent).toContain("均不支持对话");
});

it("keeps a typed model id addable after a failed read", async () => {
  draw({ checkProvider: vi.fn(async () => ({ ok: false, code: "provider.probe.path_not_found", httpStatus: 404 })) });
  await userEvent.click(inline());
  await screen.findByText("读取模型列表失败");
  await userEvent.type(search(), "relay-private-1{Enter}");
  expect((await screen.findByRole("checkbox", { name: "选用 relay-private-1" })).getAttribute("aria-checked")).toBe("true");
});

it("probes with the typed key and address from the add field too", async () => {
  const probeProvider = vi.fn(async (_url: string, _key: string) => ({ ok: true, models: ["alpha", "delta"] }));
  draw({ probeProvider: probeProvider as unknown as Port["probeProvider"] });
  await userEvent.type(screen.getByPlaceholderText("········"), "sk-new");
  await userEvent.click(inline());
  await waitFor(() => expect(probeProvider).toHaveBeenCalledWith("https://relay.example.com/v1", "sk-new"));
  await screen.findByText("delta");
});

it("holds both entries while a read runs", async () => {
  let release: (v: unknown) => void = () => {};
  const checkProvider = vi.fn(() => new Promise((r) => { release = r; }));
  draw({ checkProvider: checkProvider as unknown as Port["checkProvider"] });
  await userEvent.click(inline());
  const reading = await screen.findAllByRole("button", { name: "正在从服务商读取…" });
  expect(reading).toHaveLength(2);
  expect(reading.every((b) => (b as HTMLButtonElement).disabled)).toBe(true);
  release({ ok: true, models: ["alpha"] });
});
