// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { dropDraft } from "./feedbackdraft";
import { MockHub } from "../port/mock_hub";

afterEach(() => {
  cleanup();
  dropDraft();
});

function paneless(opts: { folders: boolean }) {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  const open = hub.open.bind(hub);
  const opened: boolean[] = [];
  let live = false;
  hub.runtimes = async () => (live ? [await open({ root: "~/projects/DeepSeek-Reasonix" })] : []);
  hub.open = async (req) => {
    live = true;
    opened.push(true);
    return open(req);
  };
  if (!opts.folders) hub.tree = async () => [];
  return { hub, opened };
}

function folderless() {
  const { hub } = paneless({ folders: false });
  const roots: string[] = [];
  hub.tree = async () => roots.map((root) => ({ root, name: root, open: false, remembered: true, sessions: [] }));
  const add = hub.addWorkspace.bind(hub);
  hub.addWorkspace = (path) => {
    roots.push(path);
    return add(path);
  };
  hub.pickFolder = async () => "~/projects/new";
  return { hub };
}

describe("feedback and settings with no pane open", () => {
  it("opens the feedback form by starting a session in the current folder", async () => {
    const { hub, opened } = paneless({ folders: true });
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /发送反馈/ }));
    expect(await screen.findByRole("dialog", { name: "反馈" })).toBeTruthy();
    expect(opened.length).toBe(1);
    expect(document.querySelector(".errbar")).toBeNull();
  });

  it("says why feedback cannot open when there is no folder to start in", async () => {
    const { hub } = paneless({ folders: false });
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /发送反馈/ }));
    const bar = await waitFor(() => {
      const el = document.querySelector(".errbar");
      expect(el).toBeTruthy();
      return el!;
    });
    expect(bar.textContent).toContain("反馈需要一个打开的会话");
    expect(screen.queryByRole("dialog", { name: "反馈" })).toBeNull();
  });

  it("drops the refusal once a folder is added", async () => {
    const { hub } = paneless({ folders: false });
    const roots: string[] = [];
    hub.tree = async () => roots.map((root) => ({ root, name: root, open: false, remembered: true, sessions: [] }));
    const add = hub.addWorkspace.bind(hub);
    hub.addWorkspace = (path) => {
      roots.push(path);
      return add(path);
    };
    hub.pickFolder = async () => "~/projects/new";
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /发送反馈/ }));
    await waitFor(() => expect(document.querySelector(".errbar")).toBeTruthy());
    await userEvent.click(await screen.findByRole("button", { name: "添加文件夹" }));
    await waitFor(() => expect(document.querySelector(".errbar")).toBeNull());
  });

  it("drops the settings refusal once a folder is added", async () => {
    const { hub } = folderless();
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /工具与集成/ }));
    await waitFor(() => expect(document.querySelector(".errbar")!.textContent).toContain("设置需要一个打开的会话"));
    await userEvent.click(await screen.findByRole("button", { name: "添加文件夹" }));
    await waitFor(() => expect(document.querySelector(".errbar")).toBeNull());
  });

  it("keeps a different error that replaced the refusal when a folder then appears", async () => {
    const { hub } = folderless();
    const add = hub.addWorkspace.bind(hub);
    let tries = 0;
    hub.addWorkspace = async (path) => {
      if (tries++ === 0) throw new Error("disk is read-only");
      return add(path);
    };
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /发送反馈/ }));
    await waitFor(() => expect(document.querySelector(".errbar")!.textContent).toContain("反馈需要"));
    await userEvent.click(await screen.findByRole("button", { name: "添加文件夹" }));
    await waitFor(() => expect(document.querySelector(".errbar")!.textContent).toContain("disk is read-only"));
    await userEvent.click(await screen.findByRole("button", { name: "添加文件夹" }));
    await waitFor(() => expect(tries).toBe(2));
    await screen.findByText("~/projects/new");
    expect(document.querySelector(".errbar")!.textContent).toContain("disk is read-only");
  });
});
