// @vitest-environment jsdom
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { RuntimeView, TreeWorkspace } from "../port/hub";
import { Workspaces } from "./Workspaces";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(cleanup);

interface Gate {
  req: { root?: string; sessionPath?: string };
  ok: () => void;
  fail: (e: Error) => void;
}

function window() {
  const hub = new MockHub();
  const gates: Gate[] = [];
  const open = hub.open.bind(hub);
  hub.open = (req) =>
    new Promise<RuntimeView>((resolve, reject) => {
      gates.push({ req, ok: () => void open(req).then(resolve), fail: reject });
    });
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  return { hub, gates };
}

const row = (name: RegExp) => screen.findByRole("treeitem", { name });
const selected = (el: HTMLElement) => el.getAttribute("aria-selected");

describe("a session row selects on the click, not on the kernel's answer", () => {
  it("marks the row selected and busy while the open is still pending", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    const target = await row(/上一次的会话/);

    await userEvent.click(target);

    expect(gates).toHaveLength(1);
    expect(selected(target)).toBe("true");
    expect(target.hasAttribute("data-busy")).toBe(true);
    expect(selected(current)).toBe("false");
    expect(document.querySelector(".panes")?.getAttribute("aria-busy")).toBe("true");

    gates[0].ok();
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(2));
    await waitFor(() => expect(target.hasAttribute("data-busy")).toBe(false));
    expect(selected(target)).toBe("true");
    expect(selected(current)).toBe("false");
    expect(document.querySelector(".panes")?.hasAttribute("aria-busy")).toBe(false);
  });

  it("rolls back to the session that was in front and shows the refusal when the open fails", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    const target = await row(/上一次的会话/);

    await userEvent.click(target);
    expect(selected(target)).toBe("true");
    gates[0].fail(new Error("session is open in another window"));

    await waitFor(() => expect(selected(target)).toBe("false"));
    expect(selected(current)).toBe("true");
    expect(target.hasAttribute("data-busy")).toBe(false);
    expect(document.querySelector(".panes")?.hasAttribute("aria-busy")).toBe(false);
    expect(await screen.findByText(/session is open in another window/)).toBeTruthy();
  });

  it("lets the last click win and ignores the earlier open when it lands late", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const first = await row(/上一次的会话/);
    await userEvent.click(await row(/my-website/));
    const second = await row(/站点改版/);

    await userEvent.click(first);
    await userEvent.click(second);
    expect(gates).toHaveLength(2);
    expect(selected(second)).toBe("true");
    expect(selected(first)).toBe("false");

    gates[1].ok();
    await waitFor(() => expect(second.hasAttribute("data-busy")).toBe(false));
    gates[0].ok();
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(3));
    expect(selected(second)).toBe("true");
    expect(selected(first)).toBe("false");
    const panes = [...document.querySelectorAll(".pane")];
    expect(panes.map((p) => p.hasAttribute("data-active"))).toEqual([false, true, false]);
  });

  it("does not let a stale failure clear the newer selection", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const first = await row(/上一次的会话/);
    await userEvent.click(await row(/my-website/));
    const second = await row(/站点改版/);

    await userEvent.click(first);
    await userEvent.click(second);
    gates[0].fail(new Error("boom"));
    await screen.findByText(/boom/);

    expect(selected(second)).toBe("true");
    expect(second.hasAttribute("data-busy")).toBe(true);
  });

  it("selects a session that already has a pane without asking the kernel", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    await userEvent.click(await row(/上一次的会话/));
    gates[0].ok();
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(2));
    const older = await row(/上一次的会话/);
    await waitFor(() => expect(selected(older)).toBe("true"));

    await userEvent.click(current);

    expect(gates).toHaveLength(1);
    expect(selected(current)).toBe("true");
    expect(selected(older)).toBe("false");
  });

  it("drops a pending open's selection when another row is focused", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    const target = await row(/上一次的会话/);

    await userEvent.click(target);
    await userEvent.click(current);

    expect(selected(current)).toBe("true");
    expect(selected(target)).toBe("false");
    gates[0].ok();
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(2));
    expect(selected(current)).toBe("true");
    expect(selected(target)).toBe("false");
  });

  it("opens from the keyboard through the same selection", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const target = await row(/上一次的会话/);
    target.focus();

    await userEvent.keyboard("{Enter}");

    expect(gates).toHaveLength(1);
    expect(selected(target)).toBe("true");
    expect(target.hasAttribute("data-busy")).toBe(true);
  });

  it("keeps the selection while a tree reload lands", async () => {
    const { hub } = window();
    const read = hub.tree.bind(hub);
    let stale = false;
    let reads = 0;
    hub.tree = async () => {
      reads++;
      const tree = await read();
      if (!stale) return tree;
      return tree.map((ws) => ({ ...ws, sessions: ws.sessions.map((x) => ({ ...x, runtimeId: undefined })) }));
    };
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    const target = await row(/上一次的会话/);

    await userEvent.click(target);
    stale = true;
    const before = reads;
    fireEvent.contextMenu(current);
    await userEvent.click(await screen.findByRole("menuitem", { name: /重命名/ }));
    const field = await screen.findByLabelText("聊天标题");
    await userEvent.type(field, "x{Enter}");
    await waitFor(() => expect(reads).toBeGreaterThan(before));

    expect(selected(target)).toBe("true");
    expect(selected(current)).toBe("false");
    expect(target.hasAttribute("data-busy")).toBe(true);
  });

  it("stops dimming the pane as soon as the opened one is in front, while the refresh is still running", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    const target = await row(/上一次的会话/);
    const list = hub.runtimes.bind(hub);
    let release: () => void = () => {};
    let held = false;
    hub.runtimes = async () => {
      if (!held) return list();
      await new Promise<void>((resolve) => (release = resolve));
      return list();
    };

    await userEvent.click(target);
    held = true;
    gates[0].ok();
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(2));
    await waitFor(() => expect(document.querySelectorAll(".pane")[1].hasAttribute("data-active")).toBe(true));

    expect(document.querySelector(".panes")?.hasAttribute("aria-busy")).toBe(false);
    expect(selected(target)).toBe("true");
    expect(target.hasAttribute("data-busy")).toBe(true);

    release();
    await waitFor(() => expect(target.hasAttribute("data-busy")).toBe(false));
    expect(selected(target)).toBe("true");
  });

  it("shares the in-flight open when the same row is clicked again after another focus cancelled it", async () => {
    const { hub, gates } = window();
    render(<App hub={hub} />);
    const current = await row(/并行会话演示/);
    await waitFor(() => expect(selected(current)).toBe("true"));
    const target = await row(/上一次的会话/);

    await userEvent.click(target);
    await userEvent.click(current);
    await userEvent.click(target);

    expect(gates).toHaveLength(1);
    expect(selected(target)).toBe("true");
    gates[0].ok();
    await waitFor(() => expect(document.querySelectorAll(".pane")[1]?.hasAttribute("data-active")).toBe(true));
    await waitFor(() => expect(target.hasAttribute("data-busy")).toBe(false));
    expect(selected(target)).toBe("true");
    expect(selected(current)).toBe("false");
  });
});

describe("a version row that is being opened", () => {
  it("is the selected row, and the row that was in front is not", async () => {
    const version = "/w/s/20260924-130000-version.jsonl";
    const tree: TreeWorkspace[] = [
      {
        root: "/w",
        name: "w",
        sessions: [{ path: "/w/s/a.jsonl", name: "a", title: "the conversation", runtimeId: "r1", versions: [{ path: version, name: "v", turns: 4 }] }],
      } as TreeWorkspace,
    ];
    const noop = async () => {};
    render(
      <Workspaces
        hub={{} as never} tree={tree} treeRead runtimes={[{ id: "r1", base: "", root: "/w", name: "w", sessionPath: "/w/s/a.jsonl" }]}
        active="r1" folded={new Set()} onFold={() => {}} reload={noop} opening={version} onOpen={noop} onFocus={() => {}}
        onClose={noop} liveIds={() => []} runs={{}} onRename={() => {}} onError={() => {}}
        adder={{ add: () => {}, close: () => {}, at: null } as never}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "+1" }));
    const copy = screen.getByText("早先版本 · 4 轮").closest("[role=treeitem]") as HTMLElement;
    const main = within(document.body).getByRole("treeitem", { name: /the conversation/ });

    expect(selected(copy)).toBe("true");
    expect(copy.hasAttribute("data-busy")).toBe(true);
    expect(selected(main)).toBe("false");
  });
});
