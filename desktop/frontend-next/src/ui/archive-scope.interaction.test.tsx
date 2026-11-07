// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { TreeSession, TreeWindow, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

async function chooseScope(name: RegExp) {
  await userEvent.click(await screen.findByRole("button", { name: "会话范围" }));
  await userEvent.click(within(document.querySelector<HTMLElement>(".session-filter-menu:not([hidden])")!).getByRole("menuitem", { name }));
}

const row = (n: number, archived: boolean): TreeSession => ({ path: `/s/${archived ? "old" : "new"}-${n}.jsonl`, name: `${archived ? "old" : "new"}-${n}`, title: `${archived ? "归档的" : "活跃的"}会话 ${n}`, turns: 2, archived });
const folder = (sessions: TreeSession[]): TreeWorkspace => ({
  root: "/w", name: "w", open: false, remembered: true, sessions, total: 2127 + 27, archived: 2127,
});

// The kernel answers by view: the active rows for the default view, the archived
// ones for scope=archived — and the archived read is the slow one.
function slowArchive() {
  const h = new MockHub();
  const build = h.portFor.bind(h);
  h.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  h.tree = async (window?: TreeWindow) => {
    if (window?.scope === "archived") {
      await gate;
      return [folder([row(1, true), row(2, true)])];
    }
    return [folder([row(1, false), row(2, false)])];
  };
  return { hub: h, release };
}

it("while the archived rows are being read the list says it is reading, not that nothing matches", async () => {
  const { hub, release } = slowArchive();
  render(<App hub={hub} />);
  await screen.findByRole("button", { name: "会话范围" });
  await screen.findByText("活跃的会话 1");
  await chooseScope(/归档/);
  expect(await screen.findByText("正在读取文件夹…")).toBeTruthy();
  expect(screen.queryByText("没有匹配的会话")).toBeNull();
  await act(async () => release());
  expect(await screen.findByText("归档的会话 1")).toBeTruthy();
  expect(screen.queryByText("正在读取文件夹…")).toBeNull();
});

it("an archive that really holds nothing still says so once the kernel has answered", async () => {
  const h = new MockHub();
  const build = h.portFor.bind(h);
  h.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  h.tree = async () => [{ ...folder([row(1, false)]), total: 1, archived: 0 }];
  render(<App hub={h} />);
  await screen.findByRole("button", { name: "会话范围" });
  await screen.findByText("活跃的会话 1");
  await chooseScope(/归档/);
  expect(await screen.findByText("没有匹配的会话")).toBeTruthy();
});

it("a long archive loads further pages in the archived view, and a search still finds an archived conversation", async () => {
  const h = new MockHub();
  const build = h.portFor.bind(h);
  h.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  const asked: TreeWindow[] = [];
  h.tree = async (window?: TreeWindow) => {
    asked.push({ ...window });
    const limit = window?.limits?.["/w"] ?? window?.limit ?? 50;
    if (window?.scope === "archived") {
      return [{ ...folder(Array.from({ length: limit }, (_, i) => row(i + 1, true))), remaining: 2127 - limit }];
    }
    if (window?.q) return [folder([row(7, true)])];
    return [folder([row(1, false)])];
  };
  render(<App hub={h} />);
  await screen.findByRole("button", { name: "会话范围" });
  await screen.findByText("活跃的会话 1");
  await chooseScope(/归档/);
  await screen.findByText("归档的会话 50");
  await userEvent.click(await screen.findByRole("button", { name: /显示更多/ }));
  await screen.findByText("归档的会话 100");
  expect(asked.at(-1)).toMatchObject({ scope: "archived", limits: { "/w": 100 } });
  await chooseScope(/全部/);
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "7");
  // The query is marked inside the title, so the title is no longer one text node.
  await waitFor(() => expect(document.querySelector(".rail")!.textContent).toContain("归档的会话 7"));
  expect(asked.at(-1)?.q).toBe("7");
});
