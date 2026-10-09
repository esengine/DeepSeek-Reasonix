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
    checkProvider: vi.fn(async () => ({ ok: true, kind: "openai", models: ["chat", "vision-chat"], vision: ["vision-chat"] })),
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
const url = () => within(detail()).getByLabelText("接口地址") as HTMLInputElement;
const win = () => within(detail()).getByLabelText("上下文窗口") as HTMLInputElement;
const vision = () => within(detail()).getByRole("button", { name: "vision-chat 的图片输入" });
const save = () => within(detail()).getByRole("button", { name: /^保存/ }) as HTMLButtonElement;
const revert = () => within(detail()).getByRole("button", { name: "还原" }) as HTMLButtonElement;
const state = () => detail().querySelector(".acts-state")!.textContent;
const rows = () => screen.getAllByRole("button").filter((b) => b.dataset.actionClick === "provider.select");

async function open() {
  await screen.findAllByText("relay.example");
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
}
async function typeWin(v: string) {
  await userEvent.clear(win());
  await userEvent.type(win(), v);
}

it("opens clean: Save and Revert are disabled and the footer says nothing is pending", async () => {
  harness();
  await open();
  expect(save().disabled).toBe(true);
  expect(revert().disabled).toBe(true);
  expect(state()).toBe("没有更改");
  expect(within(detail()).queryByRole("button", { name: "取消" })).toBeNull();
});

it("an edit shows the unsaved indicator and enables Save and Revert", async () => {
  harness();
  await open();
  await typeWin("64000");
  expect(save().disabled).toBe(false);
  expect(revert().disabled).toBe(false);
  expect(state()).toBe("有未保存的更改");
});

it("typing the stored value back, or toggling a row off and on, is clean again", async () => {
  harness();
  await open();
  await typeWin("64000");
  await typeWin("32000");
  expect(state()).toBe("没有更改");
  expect(save().disabled).toBe(true);
  const chat = within(detail()).getByRole("checkbox", { name: "选用 vision-chat" });
  await userEvent.click(chat);
  expect(state()).toBe("有未保存的更改");
  await userEvent.click(chat);
  expect(state()).toBe("没有更改");
});

it("a typed key, and a field the form cannot parse, both count as pending", async () => {
  harness();
  await open();
  const key = within(detail()).getByLabelText("API Key（留空就不动它）");
  await userEvent.type(key, "sk-test");
  expect(state()).toBe("有未保存的更改");
  await userEvent.clear(key);
  expect(state()).toBe("没有更改");
  await userEvent.click(detail().querySelector("summary")!);
  await userEvent.type(within(detail()).getByLabelText("无响应超时"), "abc");
  expect(state()).toBe("有未保存的更改");
  expect(save().disabled).toBe(true);
  expect(revert().disabled).toBe(false);
});

it("Revert puts back what is stored without writing anything", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  await userEvent.click(vision());
  await userEvent.click(revert());
  await waitFor(() => expect(win().value).toBe("32000"));
  expect(vision().getAttribute("aria-pressed")).toBe("false");
  expect(state()).toBe("没有更改");
  expect(save().disabled).toBe(true);
  expect(h.edits).toHaveLength(0);
});

it("a saved edit says Saved and disables Save; the next edit replaces the message and typing back does not bring it back", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await waitFor(() => expect(state()).toBe("已保存"));
  expect(save().disabled).toBe(true);
  expect(h.edits).toHaveLength(1);
  await typeWin("65000");
  expect(state()).toBe("有未保存的更改");
  await typeWin("64000");
  expect(state()).toBe("没有更改");
});

it("does not claim Saved before the re-read lands", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));
  expect(state()).not.toBe("已保存");
  h.releaseReads();
  await waitFor(() => expect(state()).toBe("已保存"));
});

it("a refused save keeps the draft pending, Save enabled and no Saved message", async () => {
  const h = harness({ edit: async () => { throw new HttpError(400, "bad", { code: "provider.invalid" }); } });
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await screen.findByText("保存失败");
  expect(state()).toBe("有未保存的更改");
  expect(save().disabled).toBe(false);
  expect(win().value).toBe("64000");
  expect(h.edits).toHaveLength(1);
});

it("saved-but-not-applied stays a warning, never the Saved message, and is no longer pending while Save stays available to apply it", async () => {
  harness({ edit: async () => { throw new HttpError(409, "running", { code: "provider.saved_while_running" }); } });
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await screen.findByText("已保存，尚未生效");
  expect(state()).toBe("已保存，尚未生效");
  expect(save().disabled).toBe(false);
  expect(win().value).toBe("64000");
  await typeWin("65000");
  expect(state()).toBe("有未保存的更改");
  await typeWin("64000");
  expect(state()).toBe("已保存，尚未生效");
});

it("leaving a service with unsaved edits drops them, and Saved does not follow to another service", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  await userEvent.click(rows()[1]);
  await userEvent.click(screen.getByRole("button", { name: "放弃更改并离开" }));
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(state()).toBe("没有更改");
  await userEvent.click(rows()[0]);
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
  expect(win().value).toBe("32000");
  expect(state()).toBe("没有更改");
  await typeWin("64000");
  await userEvent.click(save());
  await waitFor(() => expect(state()).toBe("已保存"));
  await userEvent.click(rows()[1]);
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(state()).toBe("没有更改");
  expect(h.edits).toHaveLength(1);
});

it("reopening the page after a save shows the stored values clean, with no Saved message", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await waitFor(() => expect(state()).toBe("已保存"));
  cleanup();
  h.view();
  await waitFor(() => expect(win().value).toBe("64000"));
  expect(state()).toBe("没有更改");
  expect(save().disabled).toBe(true);
});

it("image support found by a connection test is pending until saved, then stored", async () => {
  const h = harness();
  await open();
  await userEvent.click(within(detail()).getByRole("button", { name: "测试连接" }));
  await screen.findByText(/连上了/);
  await typeWin("64000");
  await userEvent.click(revert());
  await waitFor(() => expect(win().value).toBe("32000"));
  expect(vision().getAttribute("aria-pressed")).toBe("true");
  expect(state()).toBe("有未保存的更改");
  expect(save().disabled).toBe(false);
  await userEvent.click(save());
  await waitFor(() => expect(state()).toBe("已保存"));
  expect(h.edits[0]).toMatchObject({ vision: ["vision-chat"] });
  expect(save().disabled).toBe(true);
});
