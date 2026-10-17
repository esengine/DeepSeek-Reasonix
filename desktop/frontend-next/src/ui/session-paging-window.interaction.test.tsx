// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { TreeSession, TreeWindow, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const TOTAL = 132;
const ARCHIVED = [0, 1];
const PINNED = 5;

// A kernel that answers the way /tree does: the newest rows of the requested
// scope and word up to the folder's limit, every pinned active row whatever the
// window, and the counts for the whole folder.
function pagedHub() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  const asked: (TreeWindow | undefined)[] = [];
  let change: () => void = () => {};
  hub.onTreeChanged = (listener) => {
    change = listener;
    return () => {};
  };
  const all: TreeSession[] = Array.from({ length: TOTAL }, (_, i) => ({
    path: `/w/.reasonix/sessions/s${i}.jsonl`,
    name: `s${i}`,
    title: i >= 100 && i < 110 ? `release plan ${i}` : `chat ${i}`,
    turns: 1,
    archived: ARCHIVED.includes(i) || undefined,
    pinned: i === PINNED || undefined,
  })).reverse();
  hub.tree = async (window) => {
    asked.push(window);
    const limit = window?.limits?.["/w"] ?? window?.limit ?? 50;
    const word = (window?.q ?? "").toLowerCase();
    let taken = 0;
    let remaining = 0;
    const sessions: TreeSession[] = [];
    for (const row of all) {
      const matches =
        (window?.scope !== "archived" || !!row.archived) &&
        (window?.scope !== "active" || !row.archived) &&
        (!word || (row.title || row.name).toLowerCase().includes(word));
      const inWindow = matches && (limit === 0 || taken < limit);
      if (inWindow) taken++;
      if (inWindow || (row.pinned && !row.archived)) sessions.push(row);
      else if (matches) remaining++;
    }
    return [{ root: "/w", name: "w", total: TOTAL, archived: ARCHIVED.length, remaining, sessions }] as TreeWorkspace[];
  };
  hub.runtimes = async () => [];
  return { hub, asked, change: () => change() };
}

const filter = (name: RegExp) => screen.getAllByRole("button", { name }).find((b) => b.closest(".studio-session-segments"))!;

it("keeps the pages the reader opened across a tree refresh", async () => {
  const { hub, asked, change } = pagedHub();
  render(<App hub={hub} />);

  await userEvent.click(await screen.findByRole("button", { name: /显示更多（还有 \d+ 个）/ }));
  await waitFor(() => expect(asked.at(-1)).toMatchObject({ limit: 50, limits: { "/w": 100 }, scope: "active" }));

  await act(async () => change());
  await waitFor(() => expect(asked.at(-1)).toMatchObject({ limits: { "/w": 100 }, scope: "active" }));
});

it("filters by the word on the kernel and never asks for every row", async () => {
  const { hub, asked } = pagedHub();
  render(<App hub={hub} />);
  await screen.findByRole("button", { name: /显示更多/ });

  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "release plan");
  await waitFor(() => expect(asked.at(-1)).toMatchObject({ scope: "", q: "release plan" }));
  expect(asked.every((window) => window?.limit !== 0)).toBe(true);
  expect(await screen.findByText("release plan 109")).toBeTruthy();

  await userEvent.clear(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }));
  await waitFor(() => expect(asked.at(-1)).toMatchObject({ scope: "active", q: "" }));
});

it("keeps the full project total while its loaded window grows, without local-only filter badges", async () => {
  const { hub } = pagedHub();
  render(<App hub={hub} />);
  await screen.findByRole("button", { name: /显示更多/ });
  expect(await screen.findByLabelText(/\/ 130 会话/)).toBeTruthy();
  for (const scope of [/^全部/, /^归档/, /^置顶/]) expect(filter(scope).querySelector("b")).toBeNull();
  await userEvent.click(await screen.findByRole("button", { name: /显示更多/ }));
  await waitFor(() => expect(screen.getByLabelText(/101 \/ 130 会话/)).toBeTruthy());
});

it("reaches an archived conversation far outside the window instead of a dead end", async () => {
  const { hub, asked } = pagedHub();
  render(<App hub={hub} />);
  await screen.findByRole("button", { name: /显示更多/ });

  await userEvent.click(filter(/^归档/));
  await waitFor(() => expect(asked.at(-1)).toMatchObject({ scope: "archived" }));
  expect(await screen.findByText("chat 0")).toBeTruthy();
  expect(screen.getByText("chat 1")).toBeTruthy();
  expect(screen.queryByText("没有匹配的会话")).toBeNull();
});

it("shows the pinned conversation that is far outside the window", async () => {
  const { hub } = pagedHub();
  render(<App hub={hub} />);
  await screen.findByRole("button", { name: /显示更多/ });
  await userEvent.click(filter(/^置顶/));
  expect(await screen.findByText("chat 5")).toBeTruthy();
});
