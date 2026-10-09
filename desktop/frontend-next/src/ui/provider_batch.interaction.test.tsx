// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderEntry, ProviderModelCheck, ProviderModelCheckRequest } from "../port/port";
import { EditConn } from "./EditConn";
import { Providers, type Port } from "./Providers";

afterEach(cleanup);

const names = ["m1", "m2", "m3", "m4", "m5"];
const entry = (models = names): ProviderEntry => ({
  name: "relay", kind: "openai", baseUrl: "https://relay.example.com/v1",
  models, default: models[0], hasKey: true, inUse: false, preset: false,
});

function gate() {
  const open: Array<{ model: string; settle: (r: ProviderModelCheck | Error) => void }> = [];
  let peak = 0;
  let live = 0;
  const calls: ProviderModelCheckRequest[] = [];
  const checkProviderModel = vi.fn((req: ProviderModelCheckRequest) => new Promise<ProviderModelCheck>((resolve, reject) => {
    calls.push(req);
    live++;
    peak = Math.max(peak, live);
    open.push({
      model: req.model,
      settle: (r) => {
        live--;
        if (r instanceof Error) reject(r); else resolve(r);
      },
    });
  }));
  const finish = async (model: string, r: ProviderModelCheck | Error = { model, status: "available" }) => {
    const at = open.findIndex((o) => o.model === model);
    const [one] = open.splice(at, 1);
    await act(async () => one.settle(r));
  };
  return { checkProviderModel, calls, open, finish, peak: () => peak };
}

function draw(g: ReturnType<typeof gate>, e = entry(), extra: Partial<Port> = {}) {
  const port = { checkProviderModel: g.checkProviderModel, editProvider: vi.fn(), ...extra } as unknown as Port;
  return render(<EditConn entry={e} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
}
const all = () => screen.getByRole("button", { name: /^测试已启用模型/ }) as HTMLButtonElement;
const summary = () => document.querySelector(".msummary")?.textContent;
const live = () => document.querySelector(".msummary[role='status']");

it("tests only the ticked rows, two at a time, and the next one starts as soon as a slot frees", async () => {
  const g = gate();
  draw(g);
  await userEvent.click(screen.getByRole("checkbox", { name: "选用 m5" }));
  await userEvent.click(all());
  await waitFor(() => expect(g.calls.map((c) => c.model)).toEqual(["m1", "m2"]));
  expect(summary()).toBe("2 个验证中 · 2 个未验证");
  await g.finish("m1");
  await waitFor(() => expect(g.calls.map((c) => c.model)).toEqual(["m1", "m2", "m3"]));
  await g.finish("m2");
  await g.finish("m3");
  await waitFor(() => expect(g.calls.map((c) => c.model)).toEqual(["m1", "m2", "m3", "m4"]));
  await g.finish("m4");
  await waitFor(() => expect(all().disabled).toBe(false));
  expect(g.peak()).toBe(2);
  expect(g.calls.map((c) => c.model)).not.toContain("m5");
  expect(summary()).toBe("4 个可用");
});

it("sends the draft address and key with every request", async () => {
  const g = gate();
  draw(g, entry(["m1"]));
  await userEvent.type(screen.getByLabelText("API Key（留空就不动它）"), "sk-draft");
  await userEvent.click(all());
  await waitFor(() => expect(g.calls).toHaveLength(1));
  expect(g.calls[0]).toMatchObject({ name: "relay", model: "m1", baseUrl: "https://relay.example.com/v1", apiKey: "sk-draft", kind: "openai" });
});

it("ends with one summary over partial failures and keeps each row's own words", async () => {
  const g = gate();
  draw(g, entry(["m1", "m2", "m3", "m4"]));
  await userEvent.click(all());
  await g.finish("m1");
  await g.finish("m2", { model: "m2", status: "unavailable", reason: "not_found", httpStatus: 404, detail: "no such model" });
  await g.finish("m3", new Error("socket closed"));
  await g.finish("m4", { model: "m4", status: "unknown", reason: "rejected", httpStatus: 400, detail: "does not support tools" });
  await waitFor(() => expect(summary()).toBe("1 个可用 · 1 个不可用 · 2 个无法确定"));
  const row = (m: string) => within(document.querySelector(".mrows") as HTMLElement).getByText(m).closest(".mline") as HTMLElement;
  expect(row("m2").textContent).toContain("HTTP 404");
  expect(row("m2").textContent).toContain("no such model");
  expect(row("m4").textContent).toContain("does not support tools");
});

it("stops starting rows the moment the draft is edited and drops the answers still in flight", async () => {
  const g = gate();
  draw(g);
  await userEvent.click(all());
  await waitFor(() => expect(g.open).toHaveLength(2));
  await userEvent.type(screen.getByLabelText("上下文窗口"), "1");
  await g.finish("m1");
  await g.finish("m2");
  expect(g.calls).toHaveLength(2);
  expect(all().disabled).toBe(false);
  expect(summary()).toBe("");
  expect(screen.getByText("已停止启动新的验证：草稿已改动")).toBeTruthy();
  await userEvent.type(screen.getByLabelText("上下文窗口"), "2");
  expect(screen.queryByText("已停止启动新的验证：草稿已改动")).toBeNull();
});

it("a row ticked off mid-run also stops it", async () => {
  const g = gate();
  draw(g);
  await userEvent.click(all());
  await waitFor(() => expect(g.open).toHaveLength(2));
  await userEvent.click(screen.getByRole("checkbox", { name: "选用 m5" }));
  await g.finish("m1");
  expect(g.calls).toHaveLength(2);
});

it("holds Save, Revert, refresh and the per-row verify while it runs", async () => {
  const g = gate();
  draw(g);
  await userEvent.click(all());
  await waitFor(() => expect(g.open).toHaveLength(2));
  expect(all().disabled).toBe(true);
  expect((screen.getByRole("button", { name: "从服务商读取可用模型" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button", { name: "验证模型 m3" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button", { name: /^保存/ }) as HTMLButtonElement).disabled).toBe(true);
});

it("is unavailable with no ticked row, and says what it costs", async () => {
  const g = gate();
  draw(g, entry(["m1"]));
  expect(screen.getByText(/每个已勾选的模型各发送一次小请求/)).toBeTruthy();
  await userEvent.click(screen.getByRole("checkbox", { name: "选用 m1" }));
  expect(all().disabled).toBe(true);
});

it("clears the summary when the key changes, as the row results go", async () => {
  const g = gate();
  draw(g, entry(["m1"]));
  await userEvent.click(all());
  await g.finish("m1");
  await waitFor(() => expect(summary()).toBe("1 个可用"));
  await userEvent.type(screen.getByLabelText("API Key（留空就不动它）"), "k");
  expect(summary()).toBe("");
});

it("keeps the summary live region mounted before the first result so a single check is announced", async () => {
  const g = gate();
  draw(g, entry(["m1"]));
  const before = live();
  expect(before).not.toBeNull();
  await userEvent.click(all());
  await g.finish("m1");
  await waitFor(() => expect(summary()).toBe("1 个可用"));
  expect(live()).toBe(before);
});

it("a new run clears the stopped notice", async () => {
  const g = gate();
  draw(g);
  await userEvent.click(all());
  await waitFor(() => expect(g.open).toHaveLength(2));
  await userEvent.type(screen.getByLabelText("上下文窗口"), "1");
  expect(screen.getByText("已停止启动新的验证：草稿已改动")).toBeTruthy();
  await userEvent.click(all());
  expect(screen.queryByText("已停止启动新的验证：草稿已改动")).toBeNull();
});

it("announces results once: the summary is the live region, rows are not, and a failed row keeps its alert", async () => {
  const g = gate();
  draw(g, entry(["m1", "m2"]));
  await userEvent.click(all());
  await g.finish("m1");
  await g.finish("m2", { model: "m2", status: "unavailable", reason: "not_found" });
  await waitFor(() => expect(summary()).toBe("1 个可用 · 1 个不可用"));
  expect(document.querySelectorAll(".mevidence[aria-live]")).toHaveLength(0);
  expect(document.querySelectorAll(".mevidence[role]:not([role=alert])")).toHaveLength(0);
  expect(document.querySelectorAll(".mevidence[role=alert]")).toHaveLength(1);
  expect(document.querySelectorAll(".msummary[role='status']")).toHaveLength(1);
});

it("leaving the service while it runs stops it", async () => {
  const g = gate();
  const disk = [entry(names), { ...entry(["x"]), name: "other", baseUrl: "https://other.example/v1" }];
  const port = { providers: vi.fn(async () => disk), protocols: vi.fn(async () => []), checkProviderModel: g.checkProviderModel } as unknown as Port;
  render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}} onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
  await userEvent.click(await within(await screen.findByRole("region")).findByRole("button", { name: /^测试已启用模型/ }));
  await waitFor(() => expect(g.open).toHaveLength(2));
  const rows = screen.getAllByRole("button").filter((b) => b.dataset.actionClick === "provider.select");
  await userEvent.click(rows[1]);
  await g.finish("m1");
  await g.finish("m2");
  expect(g.calls).toHaveLength(2);
});
