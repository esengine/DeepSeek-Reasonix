// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

function harness(over: { edit?: (e: ProviderEdit) => Promise<void>; extra?: ProviderEntry[]; onProtocol?: (k: string) => void } = {}) {
  const disk = new Map<string, ProviderEntry>([["relay", stored("relay")], ["other", stored("other")], ...(over.extra ?? []).map((e) => [e.name, e] as [string, ProviderEntry])]);
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
    removeProvider: vi.fn(async (name: string) => { disk.delete(name); }),
    editProvider: vi.fn(async (e: ProviderEdit) => {
      edits.push(e);
      await over.edit?.(e);
      const had = disk.get(e.name)!;
      disk.set(e.name, { ...had, baseUrl: e.baseUrl ?? had.baseUrl, contextWindow: e.contextWindow || undefined, visionModels: e.vision });
    }),
  } as unknown as Port;
  const view = () => render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}}
    onProtocol={(_a, k) => over.onProtocol?.(k)} activeKindFor={(a) => a.kinds[0]} />);
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
const rows = () => screen.getAllByRole("button").filter((b) => b.dataset.actionClick === "provider.select");
const ask = () => screen.queryByRole("alertdialog");
const keep = () => within(ask()!).getByRole("button", { name: "保留编辑" });
const drop = () => within(ask()!).getByRole("button", { name: "放弃更改并离开" });

async function open() {
  await screen.findAllByText("relay.example");
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
}
async function typeWin(v: string) {
  await userEvent.clear(win());
  await userEvent.type(win(), v);
}
const addButton = () => screen.getByRole("button", { name: /添加模型服务/ });

it("asks before another service replaces unsaved edits, and keeping them leaves the form untouched", async () => {
  harness();
  await open();
  await typeWin("64000");
  await userEvent.click(rows()[1]);
  expect(ask()).not.toBeNull();
  expect(document.activeElement).toBe(keep());
  expect(url().value).toBe("https://relay.example/v1");
  await userEvent.click(keep());
  expect(ask()).toBeNull();
  expect(win().value).toBe("64000");
  expect(url().value).toBe("https://relay.example/v1");
});

it("Escape keeps the edits and does not close anything else", async () => {
  harness();
  await open();
  await typeWin("64000");
  await userEvent.click(rows()[1]);
  await userEvent.keyboard("{Escape}");
  expect(ask()).toBeNull();
  expect(win().value).toBe("64000");
});

it("discarding leaves for the other service and the dropped edits do not come back", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  await userEvent.click(rows()[1]);
  await userEvent.click(drop());
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(ask()).toBeNull();
  await userEvent.click(rows()[0]);
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
  expect(win().value).toBe("32000");
  expect(ask()).toBeNull();
  expect(h.edits).toHaveLength(0);
});

it("asks before Add replaces unsaved edits, and discarding opens the add form", async () => {
  harness();
  await open();
  await typeWin("64000");
  await userEvent.click(addButton());
  expect(ask()).not.toBeNull();
  expect(screen.queryByRole("region")).not.toBeNull();
  await userEvent.click(drop());
  await waitFor(() => expect(screen.queryByRole("region")).toBeNull());
  expect(ask()).toBeNull();
});

it("asks before the rename pencil of another service replaces unsaved edits", async () => {
  harness();
  await open();
  await typeWin("64000");
  const pencils = screen.getAllByRole("button").filter((b) => b.dataset.action === "provider.rename-start" && b.dataset.target !== undefined);
  await userEvent.click(pencils[1]);
  expect(ask()).not.toBeNull();
  expect(screen.queryByRole("textbox", { name: /^重命名 other/ })).toBeNull();
});

it("never asks when nothing is pending, nor after the value is typed back", async () => {
  harness();
  await open();
  await userEvent.click(rows()[1]);
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(ask()).toBeNull();
  await typeWin("64000");
  await typeWin("32000");
  await userEvent.click(rows()[0]);
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
  expect(ask()).toBeNull();
});

it("does not ask after a successful save", async () => {
  harness();
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await waitFor(() => expect(within(detail()).getByText("已保存")).toBeTruthy());
  await userEvent.click(rows()[1]);
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(ask()).toBeNull();
});

it("still asks after a refused save, because the draft is still pending", async () => {
  harness({ edit: async () => { throw new HttpError(400, "bad", { code: "provider.invalid" }); } });
  await open();
  await typeWin("64000");
  await userEvent.click(save());
  await screen.findByText("保存失败");
  await userEvent.click(rows()[1]);
  expect(ask()).not.toBeNull();
  expect(win().value).toBe("64000");
});

it("does not ask, and does not move, while the save is in flight", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));
  await userEvent.click(rows()[1]);
  expect(ask()).toBeNull();
  expect(url().value).toBe("https://relay.example/v1");
  h.releaseReads();
  await waitFor(() => expect(within(detail()).getByText("已保存")).toBeTruthy());
  await userEvent.click(rows()[1]);
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(ask()).toBeNull();
});

it("does not ask while the add form is open: its draft is the add form's own", async () => {
  harness();
  await open();
  await userEvent.click(addButton());
  await userEvent.click(rows()[1]);
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(ask()).toBeNull();
});

it("describes the question to assistive tech and returns focus to the control that asked when edits are kept", async () => {
  harness();
  await open();
  await typeWin("64000");
  const other = rows()[1];
  await userEvent.click(other);
  const box = ask()!;
  const described = document.getElementById(box.getAttribute("aria-describedby") ?? "");
  expect(described?.textContent).toBe("这个服务有未保存的更改");
  await userEvent.click(keep());
  expect(document.activeElement).toBe(other);
});

it("falls back to the first field of the form when the control that asked is gone", async () => {
  harness();
  await open();
  await typeWin("64000");
  (document.activeElement as HTMLElement).blur();
  fireEvent.click(addButton());
  await userEvent.click(keep());
  expect(document.activeElement).toBe(url());
});

it("F2 on another service's row asks first and keeping returns focus to that row", async () => {
  harness();
  await open();
  await typeWin("64000");
  rows()[1].focus();
  await userEvent.keyboard("{F2}");
  expect(ask()).not.toBeNull();
  expect(screen.queryByRole("textbox", { name: /^重命名 other/ })).toBeNull();
  await userEvent.keyboard("{Escape}");
  expect(document.activeElement).toBe(rows()[1]);
  expect(win().value).toBe("64000");
});

it("a protocol switch on the open service asks first, and discarding switches", async () => {
  const onProtocol = vi.fn();
  harness({ onProtocol, extra: [{ ...stored("relay-resp"), kind: "responses", baseUrl: "https://relay.example/v1" }] });
  await open();
  await typeWin("64000");
  await userEvent.click(within(detail()).getByRole("button", { name: /^Responses/ }));
  expect(ask()).not.toBeNull();
  expect(onProtocol).not.toHaveBeenCalled();
  await userEvent.click(drop());
  expect(onProtocol).toHaveBeenCalledWith("responses");
});

it("picking a row out of a filtered list asks the same question", async () => {
  harness({ extra: ["a", "b", "c", "d"].map((n) => stored("svc-" + n)) });
  await open();
  await typeWin("64000");
  await userEvent.type(screen.getByLabelText("搜索已添加的服务"), "svc-c");
  await userEvent.click(rows()[0]);
  expect(ask()).not.toBeNull();
  await userEvent.click(drop());
  await waitFor(() => expect(url().value).toBe("https://svc-c.example/v1"));
});

it("deleting the open service is its own explicit act: no question, and the next service shows clean", async () => {
  const h = harness();
  await open();
  await typeWin("64000");
  await userEvent.click(within(detail()).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(h.port.removeProvider).toHaveBeenCalledWith("relay"));
  expect(ask()).toBeNull();
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  expect(ask()).toBeNull();
});
