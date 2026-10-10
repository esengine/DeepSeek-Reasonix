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

const stored = (name: string, over: Partial<ProviderEntry> = {}): ProviderEntry => ({
  name,
  kind: "openai",
  baseUrl: `https://${name}.example/v1`,
  models: ["chat", "vision-chat"],
  default: "chat",
  visionModels: [],
  hasKey: true,
  inUse: name === "relay",
  preset: false,
  canSetVision: true,
  contextWindow: 32000,
  ...over,
});

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => { resolve = r; });
  return { promise, resolve };
}

// What the kernel holds. editProvider writes into it; providers() reads it back,
// and can be held so the re-read lands after the editor has remounted.
function harness(over: { edit?: (e: ProviderEdit) => Promise<void> } = {}) {
  const disk = new Map<string, ProviderEntry>([
    ["relay", stored("relay")],
    ["other", stored("other")],
  ]);
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
      disk.set(e.name, {
        ...had, baseUrl: e.baseUrl ?? had.baseUrl, contextWindow: e.contextWindow || undefined, visionModels: e.vision,
      });
    }),
  } as unknown as Port;
  render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}}
    onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
  return {
    port, edits, disk,
    holdReads: () => { hold = true; },
    releaseReads: async () => {
      hold = false;
      gates.splice(0).forEach((g) => g.resolve());
    },
  };
}

const detail = () => screen.getByRole("region");
const url = () => within(detail()).getByLabelText("接口地址") as HTMLInputElement;
const win = () => within(detail()).getByLabelText("上下文窗口") as HTMLInputElement;
const visionTag = () => within(detail()).getByRole("button", { name: "vision-chat 的图片输入" });
const save = () => within(detail()).getByRole("button", { name: /^保存/ });

async function open() {
  await screen.findAllByText("relay.example");
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
}

async function draft(nextUrl: string, nextWin: string) {
  await userEvent.clear(url());
  await userEvent.type(url(), nextUrl);
  await userEvent.clear(win());
  await userEvent.type(win(), nextWin);
  await userEvent.click(visionTag());
}

it("keeps the values just saved on screen while the list is still being re-read", async () => {
  const h = harness();
  await open();
  await draft("https://moved.example/v1", "64000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("true");

  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));

  expect(url().value).toBe("https://moved.example/v1");
  expect(win().value).toBe("64000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("true");

  await h.releaseReads();
  await waitFor(() => expect(url().value).toBe("https://moved.example/v1"));
  expect(win().value).toBe("64000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("true");
  expect(h.disk.get("relay")?.visionModels).toEqual(["vision-chat"]);
});

it("never writes the pre-save values back when Save is pressed again before the re-read lands", async () => {
  const h = harness();
  await open();
  await draft("https://moved.example/v1", "64000");
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));
  await userEvent.click(save());
  expect(h.edits).toHaveLength(1);
  await h.releaseReads();
  await waitFor(() => expect(url().disabled).toBe(false));
  expect(save().hasAttribute("disabled")).toBe(true);
  await userEvent.click(save());
  expect(h.edits).toHaveLength(1);
  expect(h.edits[0]).toMatchObject({ baseUrl: "https://moved.example/v1", contextWindow: 64000, vision: ["vision-chat"] });
});

it("does not let the form be edited between the write and the re-read", async () => {
  const h = harness();
  await open();
  await draft("https://moved.example/v1", "64000");
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));
  expect(url().disabled).toBe(true);
  await h.releaseReads();
  await waitFor(() => expect(url().disabled).toBe(false));
});

it("a refused save keeps the draft, reads nothing back and says why", async () => {
  const h = harness({ edit: async () => { throw new HttpError(400, "bad", { code: "provider.invalid" }); } });
  await open();
  const reads = (h.port.providers as ReturnType<typeof vi.fn>).mock.calls.length;
  await draft("https://moved.example/v1", "64000");
  await userEvent.click(save());
  await screen.findByText("保存失败");
  expect(url().value).toBe("https://moved.example/v1");
  expect(win().value).toBe("64000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("true");
  expect((h.port.providers as ReturnType<typeof vi.fn>).mock.calls.length).toBe(reads);
});

it("a save that is stored but not applied keeps the draft open and refreshes the list", async () => {
  const h = harness({
    edit: async () => {
      throw new HttpError(409, "running", { code: "provider.saved_while_running" });
    },
  });
  await open();
  const reads = (h.port.providers as ReturnType<typeof vi.fn>).mock.calls.length;
  await draft("https://moved.example/v1", "64000");
  await userEvent.click(save());
  await screen.findByText("已保存，尚未生效");
  await waitFor(() => expect((h.port.providers as ReturnType<typeof vi.fn>).mock.calls.length).toBe(reads + 1));
  expect(url().value).toBe("https://moved.example/v1");
  expect(win().value).toBe("64000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("true");
});

it("revert puts back what is stored", async () => {
  const h = harness();
  await open();
  await draft("https://moved.example/v1", "64000");
  await userEvent.click(within(detail()).getByRole("button", { name: "还原" }));
  await waitFor(() => expect(url().value).toBe("https://relay.example/v1"));
  expect(win().value).toBe("32000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("false");
  expect(h.edits).toHaveLength(0);
});

it("two saves in a row each show and send what was typed last", async () => {
  const h = harness();
  await open();
  await draft("https://first.example/v1", "64000");
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));
  await h.releaseReads();
  await waitFor(() => expect(url().disabled).toBe(false));

  await userEvent.clear(url());
  await userEvent.type(url(), "https://second.example/v1");
  h.holdReads();
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(2));
  expect(url().value).toBe("https://second.example/v1");
  await h.releaseReads();
  await waitFor(() => expect(url().disabled).toBe(false));
  expect(url().value).toBe("https://second.example/v1");
  expect(h.edits[1]).toMatchObject({ baseUrl: "https://second.example/v1", contextWindow: 64000, vision: ["vision-chat"] });
});

it("shows the saved values again after leaving the service and coming back", async () => {
  const h = harness();
  await open();
  await draft("https://moved.example/v1", "64000");
  await userEvent.click(save());
  await waitFor(() => expect(h.edits).toHaveLength(1));
  await waitFor(() => expect(url().disabled).toBe(false));

  const rows = screen.getAllByRole("button").filter((b) => b.dataset.actionClick === "provider.select");
  await userEvent.click(rows[1]);
  await waitFor(() => expect(url().value).toBe("https://other.example/v1"));
  await userEvent.click(screen.getAllByRole("button").filter((b) => b.dataset.actionClick === "provider.select")[0]);
  await waitFor(() => expect(url().value).toBe("https://moved.example/v1"));
  expect(win().value).toBe("64000");
  expect(visionTag().getAttribute("aria-pressed")).toBe("true");
});
