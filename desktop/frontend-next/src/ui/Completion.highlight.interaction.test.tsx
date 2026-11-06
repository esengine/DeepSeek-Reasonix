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

it("tags each row with its kind so the list can leave the obvious ones unlabelled", () => {
  render(<CompletionMenu id="m" kind="slash" query="" kb={false} active={0} onPick={() => {}} onHover={() => {}}
    items={[{ label: "/compact", insert: "/compact", kind: "builtin" }, { label: "/review", insert: "/review", kind: "subagent" }]} />);
  expect(screen.getAllByRole("option").map((o) => o.getAttribute("data-kind"))).toEqual(["builtin", "subagent"]);
});
