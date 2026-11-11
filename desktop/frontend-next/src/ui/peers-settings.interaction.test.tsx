// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PeerMessages } from "./PeerMessages";
import { usePeersSettings } from "./usePeersSettings";
import { HeldPeerMessage } from "./HeldPeerMessage";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, PeerHeld, PeersSettings } from "../port/port";

function Messages({ port }: { port: Parameters<typeof PeerMessages>[0]["port"] }) {
  return <PeerMessages port={port} settings={usePeersSettings(port)} />;
}

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const settings = (over: Partial<PeersSettings> = {}): PeersSettings => ({
  enabled: false, dispatch: false, release_projects: [], max_dispatch_depth: 1, max_dispatched_sessions: 8, open_nested: false, path: "/home/me/.reasonix/config.toml", applies: "next_turn", pending: false, available: true, ...over,
});

function portWith(initial: PeersSettings) {
  const port = new MockPort();
  let current = initial;
  vi.spyOn(port, "peersSettings").mockImplementation(async () => current);
  const save = vi.spyOn(port, "savePeersSettings").mockImplementation(async (edit) => {
    current = {
      ...current,
      enabled: edit.enabled ?? current.enabled,
      dispatch: edit.dispatch ?? current.dispatch,
      open_nested: edit.open_nested ?? current.open_nested,
      release_projects: edit.release_projects ? current.release_projects.filter((p) => edit.release_projects!.includes(p.path)) : current.release_projects,
      pending: true,
    };
    return current;
  });
  return { port: port as unknown as AgentPort, save };
}

describe("the session messages switches", () => {
  it("turns messages on from the page instead of asking for a hand-written config", async () => {
    const { port, save } = portWith(settings());
    render(<Messages port={port} />);
    const on = await screen.findByRole("switch", { name: "启用会话间消息" });
    expect(on.getAttribute("aria-checked")).toBe("false");
    expect(screen.queryByText(/\[peers\] enabled = true/)).toBeNull();
    await userEvent.click(on);
    expect(save).toHaveBeenCalledExactlyOnceWith({ enabled: true });
    await waitFor(() => expect(screen.getByRole("switch", { name: "启用会话间消息" }).getAttribute("aria-checked")).toBe("true"));
  });

  it("says a saved switch takes effect from the next turn, never at once", async () => {
    const { port } = portWith(settings());
    render(<Messages port={port} />);
    expect(screen.queryByText(/下个回合起生效/)).toBeNull();
    await userEvent.click(await screen.findByRole("switch", { name: "启用会话间消息" }));
    expect(await screen.findByText(/下个回合起生效/)).toBeTruthy();
    expect(screen.queryByText(/立即/)).toBeNull();
  });

  it("holds the hand-off switch inert until messages are on", async () => {
    const { port, save } = portWith(settings());
    render(<Messages port={port} />);
    const dispatch = await screen.findByRole("switch", { name: "允许模型把任务交给别的会话" });
    expect((dispatch as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(dispatch);
    expect(save).not.toHaveBeenCalled();
  });

  it("saves the hand-off switch on its own once messages are on", async () => {
    const { port, save } = portWith(settings({ enabled: true }));
    render(<Messages port={port} />);
    await userEvent.click(await screen.findByRole("switch", { name: "允许模型把任务交给别的会话" }));
    expect(save).toHaveBeenCalledExactlyOnceWith({ dispatch: true });
  });

  it("offers opening folders inside the project without asking, off until the user turns it on", async () => {
    const { port, save } = portWith(settings({ enabled: true, dispatch: true }));
    render(<Messages port={port} />);
    const name = "不询问直接打开本项目内的文件夹";
    const sw = await screen.findByRole("switch", { name });
    expect(sw.getAttribute("aria-checked")).toBe("false");
    expect(screen.getByText(/项目之外的文件夹仍然会询问/)).toBeTruthy();
    await userEvent.click(sw);
    expect(save).toHaveBeenCalledExactlyOnceWith({ open_nested: true });
    await waitFor(() => expect(screen.getByRole("switch", { name }).getAttribute("aria-checked")).toBe("true"));
  });

  it("holds the open-folders switch inert until the model may hand off tasks", async () => {
    const { port, save } = portWith(settings({ enabled: true, dispatch: false }));
    render(<Messages port={port} />);
    const sw = await screen.findByRole("switch", { name: "不询问直接打开本项目内的文件夹" });
    expect((sw as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(sw);
    expect(save).not.toHaveBeenCalled();
  });

  it("names the file the switches live in, and shows a refusal beside them", async () => {
    const { port } = portWith(settings({ enabled: true }));
    vi.spyOn(port, "savePeersSettings").mockRejectedValue(new HttpError(500, "x", { code: "peer.settings_unwritable", params: { detail: "/home/me/.reasonix/config.toml" } }));
    render(<Messages port={port} />);
    expect(await screen.findByText("/home/me/.reasonix/config.toml")).toBeTruthy();
    await userEvent.click(await screen.findByRole("switch", { name: "允许模型把任务交给别的会话" }));
    const alert = (await screen.findByRole("alert")).textContent ?? "";
    expect(alert).toContain("没能保存");
    expect(alert).toContain("/home/me/.reasonix/config.toml");
  });

  it("holds both switches inert where the session has no folder to open", async () => {
    const { port } = portWith(settings({ enabled: true, available: false }));
    render(<Messages port={port} />);
    expect(await screen.findByText(/此会话不支持/)).toBeTruthy();
    for (const name of ["启用会话间消息", "允许模型把任务交给别的会话"]) expect((screen.getByRole("switch", { name }) as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("remembering a project when a held message is let through", () => {
  const held: PeerHeld = { id: "m-1", fromSession: "s-1", fromTitle: "拾音", fromProject: "alpha", sentAt: "2026-10-17T10:00:00Z", body: "请核对第三节" };
  const project = { path: "/work/site", name: "site" };

  it("offers it with the project named, and says where it can be undone", () => {
    const port = new MockPort() as unknown as AgentPort;
    render(<HeldPeerMessage port={port} held={held} project={project} onDone={() => {}} />);
    expect(screen.getByRole("button", { name: /批准并记住项目「site」/ })).toBeTruthy();
    expect(screen.getByText(/\/work\/site/)).toBeTruthy();
    expect(screen.getByText(/设置 → 会话间消息/)).toBeTruthy();
  });

  it("approves once without remembering anything by default", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const resolve = vi.spyOn(port, "resolvePeerMessage");
    render(<HeldPeerMessage port={port} held={held} project={project} onDone={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "批准并交给模型" }));
    expect(resolve).toHaveBeenCalledExactlyOnceWith("m-1", true, "none");
  });

  it("remembers the receiving project only when that button is the one pressed", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const resolve = vi.spyOn(port, "resolvePeerMessage");
    render(<HeldPeerMessage port={port} held={held} project={project} onDone={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: /批准并记住项目/ }));
    expect(resolve).toHaveBeenCalledExactlyOnceWith("m-1", true, "project");
  });

  it("never offers to remember a refusal", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const resolve = vi.spyOn(port, "resolvePeerMessage");
    render(<HeldPeerMessage port={port} held={held} project={project} onDone={() => {}} />);
    expect(screen.queryByRole("button", { name: /拒绝并记住/ })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "拒绝" }));
    expect(resolve).toHaveBeenCalledExactlyOnceWith("m-1", false, "none");
  });

  it("offers no remembering where the receiving project is unknown", () => {
    const port = new MockPort() as unknown as AgentPort;
    render(<HeldPeerMessage port={port} held={held} onDone={() => {}} />);
    expect(screen.queryByRole("button", { name: /记住项目/ })).toBeNull();
  });
});
