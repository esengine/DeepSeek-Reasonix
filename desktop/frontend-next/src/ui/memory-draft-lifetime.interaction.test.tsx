// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Memory } from "./Memory";
import { MockPort } from "../port/mock";
import type { AgentPort, MemoryEdit, MemoryEntry } from "../port/port";

afterEach(cleanup);

function pending() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}

async function fixture() {
  const port = new MockPort() as unknown as AgentPort;
  const originals = (await port.memories()).memories.slice(0, 2);
  for (const [index, fact] of originals.entries()) {
    await port.saveMemory({ name: fact.name, title: `fact-${index}`, description: fact.description ?? "",
      body: `fact-${index} initial`, activation: fact.activation });
  }
  const read = port.memories.bind(port);
  vi.spyOn(port, "memories").mockImplementation(async () => {
    const catalog = await read();
    return { ...catalog, memories: catalog.memories.filter((m) => originals.some((o) => o.name === m.name)) };
  });
  const facts = (await read()).memories.filter((m) => originals.some((o) => o.name === m.name));
  const view = render(<Memory port={port} />);
  await screen.findByRole("button", { name: "fact-0" });
  return { port, facts, view };
}

function row(fact: MemoryEntry) {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('[data-action="memory.forget"]'))
    .find((button) => button.dataset.target === fact.name)!.closest<HTMLElement>(".memrow")!;
}

function control(fact: MemoryEntry, action: "save" | "restore" | "forget") {
  return row(fact).querySelector<HTMLButtonElement>(`[data-action="memory.${action}"]`)!;
}

async function open(fact: MemoryEntry) {
  if (!row(fact).querySelector(".peek")) await userEvent.click(row(fact).querySelector<HTMLButtonElement>(".nm")!);
}

async function draft(fact: MemoryEntry, body: string) {
  await open(fact);
  await userEvent.click(within(row(fact)).getByRole("button", { name: "编辑" }));
  changeBody(fact, body);
}

function changeBody(fact: MemoryEntry, body: string) {
  fireEvent.change(within(row(fact)).getByRole("textbox", { name: "正文" }), { target: { value: body } });
}

function body(fact: MemoryEntry) {
  return within(row(fact)).getByRole<HTMLTextAreaElement>("textbox", { name: "正文" }).value;
}

function edit(fact: MemoryEntry, value: string): MemoryEdit {
  return { name: fact.name, title: fact.title ?? "", description: fact.description ?? "",
    body: value, activation: fact.activation };
}

function hold(port: AgentPort, action: "save" | "restore" | "forget", name: string) {
  const gate = pending();
  if (action === "save") {
    const write = vi.isMockFunction(port.saveMemory)
      ? vi.mocked(port.saveMemory).getMockImplementation()!
      : port.saveMemory.bind(port);
    const spy = vi.spyOn(port, "saveMemory");
    spy.mockImplementation(async (entry) => {
      if (entry.name === name) await gate.promise;
      await write(entry);
    });
  } else if (action === "restore") {
    const write = vi.isMockFunction(port.restoreMemory)
      ? vi.mocked(port.restoreMemory).getMockImplementation()!
      : port.restoreMemory.bind(port);
    const spy = vi.spyOn(port, "restoreMemory");
    spy.mockImplementation(async (target, revision) => {
      if (target === name) await gate.promise;
      await write(target, revision);
    });
  } else {
    const write = vi.isMockFunction(port.forgetMemory)
      ? vi.mocked(port.forgetMemory).getMockImplementation()!
      : port.forgetMemory.bind(port);
    const spy = vi.spyOn(port, "forgetMemory");
    spy.mockImplementation(async (target) => {
      if (target === name) await gate.promise;
      await write(target);
    });
  }
  return gate;
}

async function start(fact: MemoryEntry, action: "save" | "restore" | "forget", value = "submitted") {
  if (action === "save") await draft(fact, value);
  else if (action === "restore") {
    await open(fact);
    await userEvent.click(within(row(fact)).getByRole("button", { name: "第 2 版，查看历史" }));
    await within(row(fact)).findByRole("button", { name: "恢复此版本" });
  }
  await userEvent.click(control(fact, action));
}

it("retains edits made during a save, then submits the latest exact payload", async () => {
  const { port, facts: [a], view } = await fixture();
  const gate = hold(port, "save", a.name);
  await start(a, "save");
  changeBody(a, "still writing");
  const activation = a.activation === "pinned" ? "relevant" : "pinned";
  fireEvent.change(within(row(a)).getByRole("combobox", { name: "生效时机" }), { target: { value: activation } });
  view.rerender(<Memory port={port} />);
  expect(control(a, "save").disabled).toBe(true);
  await act(async () => gate.resolve());
  expect(body(a)).toBe("still writing");
  expect(within(row(a)).getByRole<HTMLSelectElement>("combobox", { name: "生效时机" }).value).toBe(activation);
  expect(control(a, "save").disabled).toBe(false);
  await userEvent.click(control(a, "save"));
  await waitFor(() => expect(row(a).querySelector(".peek > pre")?.textContent).toBe("still writing"));
  expect(vi.mocked(port.saveMemory).mock.calls).toEqual([[edit(a, "submitted")], [{ ...edit(a, "still writing"), activation }]]);
  const current = (await port.memories()).memories.find((m) => m.name === a.name)!;
  expect(current.body).toBe("still writing");
  expect(current.revision).toBe(4);
});

it("retains another fact's draft when an earlier save completes", async () => {
  const { port, facts: [a, b] } = await fixture();
  const gate = hold(port, "save", a.name);
  await start(a, "save");
  await draft(b, "other draft");
  await act(async () => gate.resolve());
  expect(body(b)).toBe("other draft");
  expect(control(b, "save").disabled).toBe(false);
  expect(vi.mocked(port.saveMemory).mock.calls).toEqual([[edit(a, "submitted")]]);
  await userEvent.click(control(b, "save"));
  await waitFor(() => expect(row(b).querySelector(".peek > pre")?.textContent).toBe("other draft"));
  expect(vi.mocked(port.saveMemory).mock.calls).toEqual([[edit(a, "submitted")], [edit(b, "other draft")]]);
});

it("preserves a newly reopened draft even when it equals the submitted payload", async () => {
  const { port, facts: [a] } = await fixture();
  const gate = hold(port, "save", a.name);
  await start(a, "save");
  await userEvent.click(within(row(a)).getByRole("button", { name: "取消" }));
  await draft(a, "submitted");
  await act(async () => gate.resolve());
  expect(body(a)).toBe("submitted");
  expect(control(a, "save").disabled).toBe(false);
  expect(vi.mocked(port.saveMemory)).toHaveBeenCalledExactlyOnceWith(edit(a, "submitted"));
});

it("closes an unchanged submitted draft after an ordinary rerender and save", async () => {
  const { port, facts: [a], view } = await fixture();
  const gate = hold(port, "save", a.name);
  await start(a, "save");
  view.rerender(<Memory port={port} />);
  await act(async () => gate.resolve());
  expect(within(row(a)).queryByRole("textbox", { name: "正文" })).toBeNull();
  expect(row(a).querySelector(".peek > pre")?.textContent).toBe("submitted");
  expect(control(a, "forget").disabled).toBe(false);
});

it("keeps a cancelled editor closed when its pending save settles", async () => {
  const { port, facts: [a] } = await fixture();
  const gate = hold(port, "save", a.name);
  await start(a, "save");
  await userEvent.click(within(row(a)).getByRole("button", { name: "取消" }));
  await act(async () => gate.resolve());
  expect(within(row(a)).queryByRole("textbox", { name: "正文" })).toBeNull();
  expect(row(a).querySelector(".peek > pre")?.textContent).toBe("submitted");
});

it("keeps a refused draft editable and retries its exact payload", async () => {
  const { port, facts: [a] } = await fixture();
  const gate = pending();
  const write = vi.spyOn(port, "saveMemory").mockImplementationOnce(() => gate.promise);
  await start(a, "save");
  await act(async () => gate.reject(new Error("save refused")));
  expect(screen.getByText("save refused")).toBeTruthy();
  expect(body(a)).toBe("submitted");
  expect(control(a, "save").disabled).toBe(false);
  await userEvent.click(control(a, "save"));
  await waitFor(() => expect(row(a).querySelector(".peek > pre")?.textContent).toBe("submitted"));
  expect(write.mock.calls).toEqual([[edit(a, "submitted")], [edit(a, "submitted")]]);
});

it("preserves another draft and frees only the failed fact's controls", async () => {
  const { port, facts: [a, b] } = await fixture();
  const gate = hold(port, "save", a.name);
  await start(a, "save");
  await draft(b, "other draft");
  await act(async () => gate.reject(new Error("earlier save refused")));
  expect(screen.getByText("earlier save refused")).toBeTruthy();
  expect(body(b)).toBe("other draft");
  expect(control(b, "save").disabled).toBe(false);
  expect(control(a, "forget").disabled).toBe(false);
});

it.each(["save", "restore", "forget"] as const)("keeps the second save pending after the first %s succeeds", async (action) => {
  const { port, facts: [a, b] } = await fixture();
  const first = hold(port, action, a.name);
  await start(a, action);
  const second = hold(port, "save", b.name);
  await start(b, "save", "second submitted");
  expect(control(a, "forget").disabled).toBe(true);
  expect(control(b, "save").disabled).toBe(true);
  await act(async () => first.resolve());
  expect(body(b)).toBe("second submitted");
  expect(control(b, "save").disabled).toBe(true);
  expect(control(b, "forget").disabled).toBe(true);
  await userEvent.click(control(b, "save"));
  expect(vi.mocked(port.saveMemory).mock.calls.filter(([value]) => value.name === b.name)).toHaveLength(1);
  await act(async () => second.resolve());
  expect(row(b).querySelector(".peek > pre")?.textContent).toBe("second submitted");
  expect(control(b, "forget").disabled).toBe(false);
});

it.each(["save", "restore", "forget"] as const)("keeps the second save pending after the first %s fails", async (action) => {
  const { port, facts: [a, b] } = await fixture();
  const first = hold(port, action, a.name);
  await start(a, action);
  const second = hold(port, "save", b.name);
  await start(b, "save", "second submitted");
  await act(async () => first.reject(new Error("first write refused")));
  expect(screen.getByText("first write refused")).toBeTruthy();
  expect(body(b)).toBe("second submitted");
  expect(control(b, "save").disabled).toBe(true);
  expect(control(b, "forget").disabled).toBe(true);
  expect(control(a, "forget").disabled).toBe(false);
  await act(async () => second.resolve());
  expect(row(b).querySelector(".peek > pre")?.textContent).toBe("second submitted");
});

it.each(["save", "restore", "forget"] as const)("keeps the first save pending when a later %s completes first", async (action) => {
  const { port, facts: [a, b] } = await fixture();
  const first = hold(port, "save", a.name);
  await start(a, "save");
  const second = hold(port, action, b.name);
  await start(b, action, "second submitted");
  await act(async () => second.resolve());
  expect(control(a, "forget").disabled).toBe(true);
  await userEvent.click(control(a, "forget"));
  const forgotten = (await port.memories()).memories.every((m) => m.name !== a.name);
  expect(forgotten).toBe(false);
  await act(async () => first.resolve());
  expect(control(a, "forget").disabled).toBe(false);
  expect((await port.memories()).memories.find((m) => m.name === a.name)?.body).toBe("submitted");
});
