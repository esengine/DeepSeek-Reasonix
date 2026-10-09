// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { AgentPort, Checkpoint, RewindScope } from "../port/port";
import { fromHistory, type Item } from "../state/session";
import { SayCard } from "./cards/SayCard";
import { useReplyActions } from "./reply";

afterEach(cleanup);

const checkpoint = (turn: number, msgIndex: number): Checkpoint => ({ turn, msgIndex, prompt: "再试一次", files: 0 });

function draw(items: Item[], checkpoints: Checkpoint[], running = false) {
  const prepareRewind = vi.fn(async (turn: number, _scope: RewindScope) => ({ planId: `plan-${turn}`, canConversation: true }));
  const commitRewind = vi.fn(async (_planId: string) => ({}));
  const submit = vi.fn(async (_text: string) => true);
  const reloadSession = vi.fn(async () => {});
  const port = { prepareRewind, commitRewind } as unknown as AgentPort;
  function Replies() {
    const { reply } = useReplyActions({
      port, items, checkpoints, running, submit, reloadSession,
      onSettings: vi.fn(), onRunDetail: vi.fn(), onError: vi.fn(),
    });
    return <>{items.filter((item): item is Extract<Item, { t: "say" }> => item.t === "say")
      .map((item) => <SayCard key={item.id} item={item} reply={reply} />)}</>;
  }
  render(<Replies />);
  return { prepareRewind, commitRewind, submit, reloadSession };
}

async function retry(index: number) {
  await userEvent.click(screen.getAllByRole("button", { name: "重新生成" })[index]);
  await userEvent.click(screen.getByRole("menuitem", { name: /按当前配置重试/ }));
}

it.each([
  { name: "first reply", index: 0, turn: 0, warns: true },
  { name: "another reply in the first turn", index: 1, turn: 0, warns: true },
  { name: "last turn's reply", index: 2, turn: 1, warns: false },
])("retries $name against its own checkpoint with identical prompts", async ({ index, turn, warns }) => {
  const items: Item[] = [
    { t: "user", id: "u1", text: "再试一次", msgIndex: 1 },
    { t: "say", id: "a1", text: "答一", done: true },
    { t: "say", id: "a1b", text: "补充", done: true },
    { t: "user", id: "u2", text: "再试一次", msgIndex: 5 },
    { t: "say", id: "a2", text: "答二", done: true },
  ];
  const { prepareRewind, commitRewind, submit } = draw(items, [checkpoint(0, 1), checkpoint(1, 5)]);
  await userEvent.click(screen.getAllByRole("button", { name: "重新生成" })[index]);
  expect(screen.queryByText("这一轮之后的记录会被丢弃") !== null).toBe(warns);
  await userEvent.click(screen.getByRole("menuitem", { name: /按当前配置重试/ }));
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(1));
  expect(prepareRewind.mock.calls).toEqual([[turn, "conversation"]]);
  expect(commitRewind).toHaveBeenCalledWith(`plan-${turn}`);
  expect(submit).toHaveBeenCalledWith("再试一次");
});

it.each([
  { index: 0, turn: 3, text: "第一轮原问题" },
  { index: 1, turn: 8, text: "第二轮原问题" },
])("uses restored message indices to resend turn $turn", async ({ index, turn, text }) => {
  const items = fromHistory([
    { role: "system", content: "sys", msgIndex: 0 },
    { role: "user", content: "第一轮原问题", msgIndex: 1 },
    { role: "assistant", content: "旧回复", msgIndex: 2 },
    { role: "user", content: "第二轮原问题", msgIndex: 5 },
    { role: "assistant", content: "新回复", msgIndex: 6 },
  ]).items;
  const { prepareRewind, submit } = draw(items, [checkpoint(3, 1), checkpoint(8, 5)]);
  await retry(index);
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(1));
  expect(prepareRewind.mock.calls).toEqual([[turn, "conversation"]]);
  expect(submit).toHaveBeenCalledWith(text);
});

it("warns about a later turn even when that turn has no checkpoint", async () => {
  draw([
    { t: "user", id: "u1", text: "一", msgIndex: 1 },
    { t: "say", id: "a1", text: "答一", done: true },
    { t: "user", id: "u2", text: "二", msgIndex: 5 },
  ], [checkpoint(0, 1)]);
  await userEvent.click(screen.getByRole("button", { name: "重新生成" }));
  expect(screen.getByText("这一轮之后的记录会被丢弃")).toBeTruthy();
});

it("does not warn for another reply, a steer or a queued message in the last turn", async () => {
  draw([
    { t: "user", id: "u1", text: "一", msgIndex: 1 },
    { t: "say", id: "a1", text: "答一", done: true },
    { t: "user", id: "steer", text: "补充要求", steer: true },
    { t: "say", id: "a2", text: "继续回答", done: true },
    { t: "user", id: "queued", text: "待发送", pending: true },
  ], [checkpoint(0, 1)]);
  await userEvent.click(screen.getAllByRole("button", { name: "重新生成" })[0]);
  expect(screen.queryByText("这一轮之后的记录会被丢弃")).toBeNull();
});

it("removes the superseded reply before sending the original question again", async () => {
  const items: Item[] = [
    { t: "user", id: "u1", text: "原问题", msgIndex: 1 },
    { t: "say", id: "a1", text: "旧回复", done: true },
    { t: "user", id: "u2", text: "后续问题", msgIndex: 5 },
    { t: "say", id: "a2", text: "后续回复", done: true },
  ];
  const order: string[] = [];
  const port = {
    prepareRewind: vi.fn(async () => ({ planId: "first", canConversation: true })),
    commitRewind: vi.fn(async () => ({})),
    history: vi.fn(async () => [{ role: "system" as const, content: "sys", msgIndex: 0 }]),
  } as unknown as AgentPort;
  function Replies() {
    const [visible, setVisible] = useState(items);
    const reloadSession = async () => {
      setVisible(fromHistory(await port.history()).items);
      order.push("reloaded");
    };
    const submit = async (text: string) => {
      order.push("submitted");
      setVisible((rows) => [...rows, { t: "user", id: "resent", text }]);
      return true;
    };
    const { reply } = useReplyActions({
      port, items: visible, checkpoints: [checkpoint(0, 1), checkpoint(1, 5)], running: false,
      submit, reloadSession, onSettings: vi.fn(), onRunDetail: vi.fn(), onError: vi.fn(),
    });
    return <>
      <div data-testid="questions">{visible.filter((i) => i.t === "user").map((i) => i.text).join("|")}</div>
      {visible.filter((i): i is Extract<Item, { t: "say" }> => i.t === "say")
        .map((i) => <SayCard key={i.id} item={i} reply={reply} />)}
    </>;
  }
  const view = render(<Replies />);
  await retry(0);
  await waitFor(() => expect(view.container.querySelectorAll('[data-k="say"]')).toHaveLength(0));
  expect(screen.getByTestId("questions").textContent).toBe("原问题");
  expect(order).toEqual(["reloaded", "submitted"]);
});

it("hides retry when the reply has no checkpoint or a task is running", () => {
  const items: Item[] = [
    { t: "user", id: "u1", text: "一", msgIndex: 1 },
    { t: "say", id: "a1", text: "答一", done: true },
    { t: "user", id: "u2", text: "二", msgIndex: 5 },
    { t: "say", id: "a2", text: "答二", done: true },
  ];
  const view = draw(items, [checkpoint(0, 1)]);
  expect(screen.getAllByRole("button", { name: "重新生成" })).toHaveLength(1);
  view.prepareRewind.mockClear();
  cleanup();
  draw(items, [checkpoint(0, 1), checkpoint(1, 5)], true);
  expect(screen.queryByRole("button", { name: "重新生成" })).toBeNull();
});

function probe(initial: Item[], checkpoints: Checkpoint[]) {
  const seen: ReturnType<typeof useReplyActions>["reply"][] = [];
  const port = {} as unknown as AgentPort;
  const common = { port, checkpoints, submit: async () => true, reloadSession: async () => {}, onSettings: () => {}, onRunDetail: () => {}, onError: () => {} };
  function Probe({ items, running }: { items: Item[]; running: boolean }) {
    seen.push(useReplyActions({ ...common, items, running }).reply);
    return null;
  }
  const view = render(<Probe items={initial} running={false} />);
  return { seen, again: (items: Item[], running = false) => view.rerender(<Probe items={items} running={running} />) };
}

const settled: Item[] = [
  { t: "user", id: "u1", text: "问", msgIndex: 1 },
  { t: "say", id: "a1", text: "答", done: true },
];

it("keeps the same reply actions while a streamed answer grows, so settled rows are not redrawn", () => {
  const live: Item = { t: "say", id: "a2", text: "", done: false };
  const { seen, again } = probe([...settled, live], [checkpoint(0, 1)]);
  for (const text of ["一", "一二", "一二三"]) again([...settled, { ...live, text }], false);
  expect(new Set(seen).size).toBe(1);
});

it("hands out new reply actions when what can be regenerated changes", () => {
  const { seen, again } = probe(settled, [checkpoint(0, 1)]);
  again(settled, true);
  expect(seen[1]).not.toBe(seen[0]);
  expect(seen[1].canRegenerate("a1")).toBe(false);
  again(settled, false);
  const next: Item[] = [...settled, { t: "user", id: "u2", text: "再问", msgIndex: 3 }, { t: "say", id: "a2", text: "答二", done: true }];
  again(next, false);
  expect(seen[seen.length - 1]).not.toBe(seen[seen.length - 2]);
  expect(seen[seen.length - 1].hasLaterTurns("a1")).toBe(true);
});
