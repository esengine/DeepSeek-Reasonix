// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { HttpError, type ProviderEdit, type ProviderEntry } from "../port/port";
import { Providers, type Port } from "./Providers";

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const stored = (name: string): ProviderEntry => ({
  name, kind: "openai", baseUrl: `https://${name}.example/v1`, models: ["chat", "vision-chat"], default: "chat",
  visionModels: [], hasKey: true, inUse: name === "relay", preset: false, canSetVision: true, contextWindow: 32000,
});

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => { resolve = r; });
  return { promise, resolve };
}

function harness(over: { edit?: (e: ProviderEdit) => Promise<void> } = {}) {
  const disk = new Map<string, ProviderEntry>([["relay", stored("relay")], ["other", stored("other")]]);
  const gates: Array<ReturnType<typeof deferred>> = [];
  const edits: ProviderEdit[] = [];
  let hold = false;
  const port = {
    providers: vi.fn(async () => {
      if (hold) {
        const gate = deferred();
        gates.push(gate);
        await gate.promise;
      }
      return [...disk.values()].map((e) => ({ ...e }));
    }),
    protocols: vi.fn(async () => []),
    editProvider: vi.fn(async (e: ProviderEdit) => {
      edits.push(e);
      await over.edit?.(e);
      const had = disk.get(e.name)!;
      disk.set(e.name, { ...had, baseUrl: e.baseUrl ?? had.baseUrl, contextWindow: e.contextWindow || undefined, visionModels: e.vision });
    }),
  } as unknown as Port;
  const view = () => render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}}
    onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
  view();
  return {
    port, edits, disk, view,
    holdReads: () => { hold = true; },
    releaseReads: () => { hold = false; gates.splice(0).forEach((g) => g.resolve()); },
  };
}

const detail = () => screen.getByRole("region");
const win = () => within(detail()).getByLabelText("上下文窗口") as HTMLInputElement;
const url = () => within(detail()).getByLabelText("接口地址") as HTMLInputElement;
const save = () => within(detail()).getByRole("button", { name: /^保存/ }) as HTMLButtonElement;
const bar = () => detail().querySelector(".acts-bar") as HTMLElement;
const status = () => bar().querySelector(".acts-state") as HTMLElement;

async function open() {
  await screen.findAllByText("relay.example");
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
}
async function typeWin(v: string) {
  await userEvent.clear(win());
  await userEvent.type(win(), v);
}

it("keeps Save, Revert and the status in one bar that is the last thing in the form", async () => {
  harness();
  await open();
  const form = detail().querySelector("fieldset") as HTMLElement;
  expect(form.lastElementChild).toBe(bar());
  const focusables = [...form.querySelectorAll<HTMLElement>("button, input, select, textarea, summary")].filter((e) => !e.matches(":disabled"));
  const inBar = [...bar().querySelectorAll("button")];
  expect(inBar.map((b) => b.textContent)).toEqual(["保存", "还原"]);
  expect(form.contains(bar())).toBe(true);
  expect(bar().previousElementSibling?.querySelector("input, select, textarea, summary, button")).not.toBeNull();
  expect(focusables.length).toBeGreaterThan(0);
});

it("tabs from the last control of the form to Save then Revert", async () => {
  harness();
  await open();
  await typeWin("64000");
  const form = detail().querySelector("fieldset") as HTMLElement;
  const order = [...form.querySelectorAll<HTMLElement>("button, input, select, textarea, summary")].filter((e) => !e.matches(":disabled"));
  expect(order.slice(-2).map((e) => e.textContent)).toEqual(["保存", "还原"]);
});

it("states every phase in one persistent status region with a dot that carries the phase", async () => {
  const h = harness();
  await open();
  const region = status();
  expect(region.getAttribute("role")).toBe("status");
  expect(bar().querySelectorAll("[role=status].acts-state")).toHaveLength(1);
  expect([region.textContent, region.dataset.state]).toEqual(["没有更改", "clean"]);
  await typeWin("64000");
  expect([status().textContent, status().dataset.state]).toEqual(["有未保存的更改", "dirty"]);
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(status().dataset.state).toBe("saving"));
  expect(status().textContent).toBe("正在保存…");
  h.releaseReads();
  await waitFor(() => expect(status().dataset.state).toBe("saved"));
  expect(status().textContent).toBe("已保存");
  expect(status().querySelector("i[aria-hidden=true]")).not.toBeNull();
});

it("keeps a refused save red and its reason in the same bar", async () => {
  harness({ edit: async () => { throw new HttpError(400, "bad", { code: "provider.invalid" }); } });
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await screen.findByText("保存失败");
  expect([status().textContent, status().dataset.state]).toEqual(["有未保存的更改", "failed"]);
  expect(bar().contains(screen.getByText("保存失败"))).toBe(true);
});

it("says saved-but-not-applied once, in the status, with the reason beside it", async () => {
  harness({ edit: async () => { throw new HttpError(409, "running", { code: "provider.saved_while_running" }); } });
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await waitFor(() => expect(status().dataset.state).toBe("unapplied"));
  expect(screen.getAllByText("已保存，尚未生效")).toHaveLength(1);
  expect(status().textContent).toBe("已保存，尚未生效");
  expect(bar().textContent).toContain("已保存。当前对话还有未结束的工作");
  expect(save().disabled).toBe(false);
});

it("Ctrl+S and Cmd+S save a pending draft once, and do nothing when clean or while saving", async () => {
  const h = harness();
  await open();
  await userEvent.click(win());
  await userEvent.keyboard("{Control>}s{/Control}");
  expect(h.edits).toHaveLength(0);
  await typeWin("64000");
  h.holdReads();
  await userEvent.keyboard("{Control>}s{/Control}");
  await waitFor(() => expect(h.edits).toHaveLength(1));
  await userEvent.keyboard("{Meta>}s{/Meta}");
  expect(h.edits).toHaveLength(1);
  h.releaseReads();
  await waitFor(() => expect(status().dataset.state).toBe("saved"));
  await userEvent.click(win());
  await userEvent.keyboard("{Meta>}s{/Meta}");
  expect(h.edits).toHaveLength(1);
});

it("the add form shares the bar: its buttons and its save failure sit together at the end", async () => {
  vi.stubGlobal("fetch", vi.fn(async (u: string, init?: RequestInit) => {
    if (u === "/k/providers/protocols") return Response.json([{ kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true }]);
    if (u === "/k/providers" && init?.method === "POST") return Response.json({ code: "provider.invalid", error: "bad" }, { status: 400 });
    throw new Error(u);
  }));
  const { AddProvider } = await import("./AddProvider");
  const { SsePort } = await import("../port/sse");
  render(<AddProvider port={new SsePort("/k")} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  await screen.findByLabelText("接口协议");
  await userEvent.type(screen.getByLabelText("来源名称"), "relay-new");
  await userEvent.type(screen.getByLabelText("接口地址"), "https://relay.example.com/v1");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "m1{enter}");
  await userEvent.click(screen.getByRole("button", { name: "添加来源" }));
  const failure = await screen.findByText("无法保存");
  const addBar = document.querySelector(".acts-bar") as HTMLElement;
  expect(addBar.contains(failure)).toBe(true);
  expect(addBar.contains(screen.getByRole("button", { name: "添加来源" }))).toBe(true);
  expect(addBar.parentElement?.lastElementChild).toBe(addBar);
});

it("names the shortcut's action the same as the Save button's", async () => {
  harness();
  await open();
  const form = detail().querySelector("fieldset") as HTMLElement;
  expect(form.dataset.actionKeydown).toBe("provider.save");
  expect(form.dataset.actionKeydown).toBe(save().dataset.action);
});
