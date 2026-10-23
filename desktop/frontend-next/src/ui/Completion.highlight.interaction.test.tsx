// @vitest-environment jsdom
import { useLayoutEffect, useState } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { CompletionMenu, useCompletion } from "./Completion";
import type { AgentPort, Completion } from "../port/port";

afterEach(cleanup);

it.each(["ref", "slash"] as const)("publishes refreshed %s candidates with their default keyboard highlight", async (kind) => {
  const snapshots: { query: string | undefined; active: number; kb: boolean }[] = [];
  const port = { complete: vi.fn(async (text: string): Promise<Completion> => ({
    kind, from: 0, to: text.length, query: text,
    items: ["first", "second"].map((label) => ({ label: text + label, insert: text + label })),
  })) } as unknown as AgentPort;
  function Probe() {
    const [text, setText] = useState(kind === "ref" ? "@a" : "/a");
    const menu = useCompletion(port, text, text.length, () => {});
    useLayoutEffect(() => {
      if (menu.open) snapshots.push({ query: menu.completion.query, active: menu.active, kb: menu.kb });
    }, [menu.completion, menu.active, menu.kb, menu.open]);
    return <>
      <input aria-label="draft" value={text} onChange={(e) => setText(e.target.value)}
        onKeyDown={() => menu.move(1)} />
      {menu.open && <CompletionMenu id="m" items={menu.completion.items} active={menu.active}
        kind={kind} query={text} kb={menu.kb} onPick={() => {}} onHover={menu.hover} />}
    </>;
  }
  render(<Probe />);
  const box = screen.getByRole("textbox", { name: "draft" });
  await screen.findByRole("option", { name: /second/ });
  fireEvent.keyDown(box, { key: "ArrowDown" });
  expect(screen.getByRole("option", { name: /second/ }).getAttribute("aria-selected")).toBe("true");
  fireEvent.mouseMove(screen.getByRole("option", { name: /second/ }));
  const next = kind === "ref" ? "@ab" : "/ab";
  fireEvent.change(box, { target: { value: next } });
  await screen.findByRole("option", { name: next + "first" });
  expect(snapshots.filter((s) => s.query === next)).toEqual([
    { query: next, active: 0, kb: true },
  ]);
});

it.each(["ref", "slash"] as const)("keeps %s navigation made as soon as the rows commit", async (kind) => {
  const text = kind === "ref" ? "@a" : "/a";
  const port = { complete: async (): Promise<Completion> => ({
    kind, from: 0, to: text.length,
    items: ["first", "second"].map((label) => ({ label, insert: label })),
  }) } as unknown as AgentPort;
  function Probe() {
    const menu = useCompletion(port, text, text.length, () => {});
    useLayoutEffect(() => {
      if (menu.completion.items.length) menu.move(1);
    }, [menu.completion]);
    return <output>{menu.completion.items[menu.active]?.label}</output>;
  }
  await act(async () => { render(<Probe />); });
  expect(screen.getByRole("status").textContent).toBe("second");
});

it.each(["Enter", "Tab"])("keeps the exact command ahead of longer matches when accepting with %s", async (key) => {
  const typed = { label: "/reload", insert: "/reload", hint: "Reload runtime", kind: "builtin" };
  const alternative = { label: "/reload-cmd", insert: "/reload-cmd", hint: "Reload commands", kind: "builtin" };
  const port = { complete: async (): Promise<Completion> => ({
    kind: "slash", from: 0, to: 7, query: "/reload", typed, items: [alternative],
  }) } as unknown as AgentPort;
  const apply = vi.fn();
  function Probe() {
    const menu = useCompletion(port, "/reload", 7, apply);
    return <>
      <input aria-label="draft" defaultValue="/reload" onKeyDown={(e) => {
        if (e.key === "ArrowDown") menu.move(1);
        if (e.key === "Enter" || e.key === "Tab") { e.preventDefault(); menu.accept(); }
        if (e.key === "Escape") menu.dismiss();
      }} />
      {menu.open && <CompletionMenu id="m" items={menu.completion.items} active={menu.active}
        kind="slash" query="/reload" kb={menu.kb} onPick={menu.accept} onHover={menu.hover} />}
    </>;
  }
  render(<Probe />);
  await screen.findByRole("option", { name: /Reload commands/ });
  const rows = screen.getAllByRole("option");
  expect(rows.map((row) => row.textContent)).toEqual([
    expect.stringContaining("Reload runtime"), expect.stringContaining("Reload commands"),
  ]);
  expect(rows[0].getAttribute("aria-selected")).toBe("true");
  const box = screen.getByRole("textbox", { name: "draft" });
  fireEvent.keyDown(box, { key: "ArrowDown" });
  fireEvent.keyDown(box, { key });
  expect(apply).toHaveBeenLastCalledWith("/reload-cmd", 11);
  fireEvent.keyDown(box, { key: "Escape" });
  expect(screen.queryByRole("listbox")).toBeNull();
});

it("offers an exact command even when no alternatives remain", async () => {
  const port = { complete: async (): Promise<Completion> => ({
    kind: "slash", from: 0, to: 7, typed: { label: "/reload", insert: "/reload" }, items: [],
  }) } as unknown as AgentPort;
  function Probe() {
    const menu = useCompletion(port, "/reload", 7, () => {});
    return menu.open && <CompletionMenu id="m" items={menu.completion.items} active={menu.active}
      kind="slash" query="/reload" kb={menu.kb} onPick={menu.accept} onHover={menu.hover} />;
  }
  render(<Probe />);
  expect(await screen.findByRole("option")).toHaveProperty("textContent", expect.stringContaining("/reload"));
});
