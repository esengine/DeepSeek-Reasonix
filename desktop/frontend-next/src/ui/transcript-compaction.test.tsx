// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import type { Item } from "../state/session";

const renders = vi.hoisted(() => new Map<string, number>());
vi.mock("./cards/SayCard", () => ({
  SayCard: ({ item }: { item: { id: string; text: string } }) => {
    renders.set(item.id, (renders.get(item.id) ?? 0) + 1);
    return <p data-say={item.id}>{item.text}</p>;
  },
}));

afterEach(() => {
  cleanup();
  renders.clear();
});

const noop = async () => undefined as never;
const same = { entering: [] as string[], onEntered: () => {}, waiting: {}, scroll: { current: null }, onExtInvoke: () => {}, checkpoints: new Map(), onOpenProject: () => {}, onKeepHere: () => {}, takeovers: {} };
const view = (items: Item[]) => (
  <Transcript items={items} {...same} revision={1} hidden={false} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop}
    onExtSubmit={noop} onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop} onCommitFileRevert={noop} needsProject={false} />
);


const turns = (n: number): Item[] => Array.from({ length: n }, (_, i): Item[] => [
  { t: "user", id: `u${i}`, text: `q${i}`, authoredTurn: i, msgIndex: i * 2 },
  { t: "say", id: `s${i}`, text: `a${i}`, done: true },
]).flat();
const squash = (id: string, messages = 8): Item => ({ t: "compaction", id, c: { messages }, done: true });
const saying = () => [...document.querySelectorAll("[data-say]")].map((e) => e.getAttribute("data-say"));

describe("earlier compactions in a transcript", () => {
  it("keeps everything on screen after the first compaction", () => {
    render(view([...turns(2), squash("c1"), ...turns(1)].map((it, i) => (it.t === "say" ? { ...it, id: `s${i}` } : it))));
    expect(screen.queryByRole("button", { name: /查看被折叠的内容/ })).toBeNull();
  });

  it("folds only the stretch before the second-latest compaction, behind one bar that opens and closes it", () => {
    const items: Item[] = [
      { t: "user", id: "u0", text: "q0", authoredTurn: 0, msgIndex: 0 }, { t: "say", id: "old", text: "oldest", done: true },
      squash("c1"),
      { t: "user", id: "u1", text: "q1", authoredTurn: 1, msgIndex: 2 }, { t: "say", id: "mid", text: "kept", done: true },
      squash("c2"),
      { t: "user", id: "u2", text: "q2", authoredTurn: 2, msgIndex: 4 }, { t: "say", id: "new", text: "latest", done: true },
    ];
    render(view(items));
    expect(saying()).toEqual(["mid", "new"]);
    const bar = screen.getByRole("button", { name: /较早的 2 条消息已归档/ });
    expect(bar.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(bar);
    expect(saying()).toEqual(["old", "mid", "new"]);
    expect(screen.getByRole("button", { name: /收起较早的内容/ }).getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: /收起较早的内容/ }));
    expect(saying()).toEqual(["mid", "new"]);
  });
});
