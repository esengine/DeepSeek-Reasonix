// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Memory } from "./Memory";
import { MockPort } from "../port/mock";
import type { AgentPort, MemoryEntry } from "../port/port";

afterEach(cleanup);
let names: string[];

function pending<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}

async function fixture(count = 1) {
  const port = new MockPort() as unknown as AgentPort;
  const originals = (await port.memories()).memories.slice(0, count);
  names = originals.map((m) => m.name);
  for (const [index, fact] of originals.entries()) {
    await port.saveMemory({ name: fact.name, title: `fact-${index}`, description: fact.description ?? "",
      body: `fact-${index} revision two`, activation: fact.activation });
  }
  const read = port.memories.bind(port);
  const catalog = vi.spyOn(port, "memories").mockImplementation(async () => {
    const current = await read();
    return { ...current, memories: current.memories.filter((m) => names.includes(m.name)) };
  });
  const facts = (await read()).memories.filter((m) => originals.some((o) => o.name === m.name));
  const revisions = vi.spyOn(port, "memoryRevisions");
  const view = render(<Memory port={port} />);
  await screen.findByRole("button", { name: "fact-0" });
  return { port, originals, facts, catalog, revisions, view };
}

function row(index = 0) {
  return Array.from(document.querySelectorAll<HTMLElement>(".memrow"))
    .find((el) => el.querySelector<HTMLButtonElement>('[data-action="memory.forget"]')?.dataset.target === names[index])!;
}

async function expand(index = 0) {
  if (!row(index).querySelector(".peek")) await userEvent.click(row(index).querySelector<HTMLButtonElement>(".nm")!);
}

async function history(revision: number, index = 0) {
  await expand(index);
  const collapse = within(row(index)).queryByRole("button", { name: "收起旧版本" });
  if (collapse) await userEvent.click(collapse);
  await userEvent.click(within(row(index)).getByRole("button", { name: `第 ${revision} 版，查看历史` }));
}

async function draft(body: string, index = 0) {
  await expand(index);
  await userEvent.click(within(row(index)).getByRole("button", { name: "编辑" }));
  fireEvent.change(within(row(index)).getByRole("textbox", { name: "正文" }), { target: { value: body } });
}

async function save(body: string, index = 0) {
  await draft(body, index);
  await userEvent.click(within(row(index)).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(row(index).querySelector(".peek > pre")?.textContent).toBe(body));
}

function versions(index = 0) {
  return Array.from(row(index).querySelectorAll(".histrow .rev"), (el) => el.textContent);
}

async function restore(revision: number, index = 0) {
  const entry = Array.from(row(index).querySelectorAll<HTMLElement>(".histrow"))
    .find((el) => within(el).queryByText(`第 ${revision} 版`))!;
  await userEvent.click(within(entry).getByRole("button", { name: "恢复此版本" }));
}

it("offers every retained intermediate save and restores the selected revision by appending", async () => {
  const { port, facts, revisions } = await fixture();
  await history(2);
  await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
  await save("revision three");
  await save("revision four");
  await history(4);
  await waitFor(() => expect(versions()).toEqual(["第 3 版", "第 2 版", "第 1 版"]));
  const write = vi.spyOn(port, "restoreMemory");
  await restore(3);
  await waitFor(() => expect(row().querySelector(".peek > pre")?.textContent).toBe("revision three"));
  expect(write).toHaveBeenCalledExactlyOnceWith(facts[0].name, 3);
  await history(5);
  await waitFor(() => expect(versions()).toEqual(["第 4 版", "第 3 版", "第 2 版", "第 1 版"]));
  expect(revisions).toHaveBeenCalledTimes(4);
  expect((await port.memoryRevisions(facts[0].name)).map((m) => m.revision ?? 1)).toEqual([5, 4, 3, 2, 1]);
});

it.each(["before", "after"])("rejects pre-save history arriving %s the new history response", async (timing) => {
  const { port, facts, revisions } = await fixture();
  const oldList = await port.memoryRevisions(facts[0].name);
  revisions.mockClear();
  const old = pending<MemoryEntry[]>();
  revisions.mockReturnValueOnce(old.promise);
  await history(2);
  await save("revision three");
  await save("revision four");
  if (timing === "before") await act(async () => old.resolve(oldList));
  await history(4);
  await waitFor(() => expect(versions()).toEqual(["第 3 版", "第 2 版", "第 1 版"]));
  if (timing === "after") await act(async () => old.resolve(oldList));
  expect(versions()).toEqual(["第 3 版", "第 2 版", "第 1 版"]);
  expect(revisions).toHaveBeenCalledTimes(2);
});

it("keeps fresh history loading when a pre-save history request fails", async () => {
  const { port, facts, revisions } = await fixture();
  const read = port.memoryRevisions.bind(port);
  const old = pending<MemoryEntry[]>();
  const fresh = pending<MemoryEntry[]>();
  revisions.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
  await history(2);
  await save("revision three");
  await history(3);
  await act(async () => old.reject(new Error("obsolete history refusal")));
  expect(within(row()).getByText("正在读取历史版本…")).toBeTruthy();
  expect(screen.queryByText("obsolete history refusal")).toBeNull();
  await act(async () => fresh.resolve(await read(facts[0].name)));
  expect(versions()).toEqual(["第 2 版", "第 1 版"]);
});

it.each(["success", "failure"])("ignores a superseded history %s after restoring a revision", async (outcome) => {
  const { port, facts, revisions } = await fixture();
  const oldList = await port.memoryRevisions(facts[0].name);
  const old = pending<MemoryEntry[]>();
  revisions.mockReturnValueOnce(old.promise);
  await history(2);
  await history(2);
  await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
  await restore(1);
  await waitFor(() => expect(row().querySelector(".peek > pre")?.textContent).toBe(oldList[1].body?.trim()));
  await save("after restore");
  await history(4);
  await waitFor(() => expect(versions()).toEqual(["第 3 版", "第 2 版", "第 1 版"]));
  await act(async () => outcome === "success" ? old.resolve(oldList) : old.reject(new Error("obsolete restore history")));
  expect(versions()).toEqual(["第 3 版", "第 2 版", "第 1 版"]);
  expect(screen.queryByText("obsolete restore history")).toBeNull();
});

it("preserves a refused save's draft and cache, then retries the exact edit", async () => {
  const { port, facts, revisions } = await fixture();
  await history(2);
  await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
  const write = vi.spyOn(port, "saveMemory").mockRejectedValueOnce(new Error("save refused"));
  await draft("retry body");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("save refused");
  expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "正文" }).value).toBe("retry body");
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await history(2);
  expect(versions()).toEqual(["第 1 版"]);
  expect(revisions).toHaveBeenCalledTimes(1);
  await draft("retry body");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(write.mock.calls).toEqual([0, 1].map(() => [{ name: facts[0].name, title: "fact-0",
    description: facts[0].description ?? "", body: "retry body", activation: facts[0].activation }]));
  await waitFor(() => expect(row().querySelector(".peek > pre")?.textContent).toBe("retry body"));
  await history(3);
  await waitFor(() => expect(versions()).toEqual(["第 2 版", "第 1 版"]));
  expect(revisions).toHaveBeenCalledTimes(2);
});

it("keeps history and the exact restore target after refusal", async () => {
  const { port, facts, revisions } = await fixture();
  await history(2);
  await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
  const write = vi.spyOn(port, "restoreMemory").mockRejectedValueOnce(new Error("restore refused"));
  await restore(1);
  await screen.findByText("restore refused");
  expect(versions()).toEqual(["第 1 版"]);
  expect(revisions).toHaveBeenCalledTimes(1);
  await restore(1);
  expect(write.mock.calls).toEqual([[facts[0].name, 1], [facts[0].name, 1]]);
  await waitFor(() => expect(row().querySelector(".peek > pre")?.textContent).not.toBe("fact-0 revision two"));
  await history(3);
  await waitFor(() => expect(versions()).toEqual(["第 2 版", "第 1 版"]));
});

it("keeps a refused forget's cached history and retries the same fact", async () => {
  const { port, facts, revisions } = await fixture();
  await history(2);
  await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
  const write = vi.spyOn(port, "forgetMemory").mockRejectedValueOnce(new Error("forget refused"));
  await userEvent.click(within(row()).getByRole("button", { name: "忘记" }));
  await screen.findByText("forget refused");
  expect(versions()).toEqual(["第 1 版"]);
  expect(revisions).toHaveBeenCalledTimes(1);
  await userEvent.click(within(row()).getByRole("button", { name: "忘记" }));
  expect(await screen.findByText("暂无记录。")).toBeTruthy();
  expect(write.mock.calls).toEqual([[facts[0].name], [facts[0].name]]);
});

it("does not surface an obsolete history refusal after forgetting its fact", async () => {
  const { revisions } = await fixture(2);
  const old = pending<MemoryEntry[]>();
  revisions.mockReturnValueOnce(old.promise);
  await history(2);
  await userEvent.click(within(row()).getByRole("button", { name: "忘记" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "fact-0" })).toBeNull());
  await act(async () => old.reject(new Error("forgotten history refusal")));
  expect(screen.queryByText("forgotten history refusal")).toBeNull();
  expect(screen.getByRole("button", { name: "fact-1" })).toBeTruthy();
});

it.each(["save", "restore", "forget"])("preserves another fact's in-flight history across %s", async (action) => {
  const { port, facts, revisions } = await fixture(2);
  const otherList = await port.memoryRevisions(facts[1].name);
  revisions.mockClear();
  const other = pending<MemoryEntry[]>();
  if (action === "restore") {
    await history(2);
    await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
    const write = pending<void>();
    const originalRestore = port.restoreMemory.bind(port);
    vi.spyOn(port, "restoreMemory").mockImplementationOnce(async (name, revision) => {
      await write.promise;
      await originalRestore(name, revision);
    });
    await restore(1);
    revisions.mockReturnValueOnce(other.promise);
    await history(2, 1);
    await act(async () => write.resolve());
  } else {
    revisions.mockReturnValueOnce(other.promise);
    await history(2, 1);
    if (action === "save") { await save("fact zero saved"); await expand(1); }
    else await userEvent.click(within(row()).getByRole("button", { name: "忘记" }));
  }
  await act(async () => other.resolve(otherList));
  expect(versions(1)).toEqual(["第 1 版"]);
  expect(revisions.mock.calls.filter(([name]) => name === facts[1].name)).toHaveLength(1);
});

it("reuses unchanged history across rerenders, collapsing and a cancelled edit", async () => {
  const { port, revisions, view } = await fixture();
  await history(2);
  await waitFor(() => expect(versions()).toEqual(["第 1 版"]));
  view.rerender(<Memory port={port} />);
  await draft("cancelled body");
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await history(2);
  expect(versions()).toEqual(["第 1 版"]);
  expect(revisions).toHaveBeenCalledTimes(1);
  expect(row().querySelector(".peek > pre")?.textContent).toBe("fact-0 revision two");
});
