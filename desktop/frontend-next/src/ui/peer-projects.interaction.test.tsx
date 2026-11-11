// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PeerProjects } from "./PeerProjects";
import { usePeersSettings } from "./usePeersSettings";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import { boot, STORAGE } from "../i18n";
import type { TreeWorkspace } from "../port/hub";
import type { AgentPort, PeersSettings } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function Projects({ port, hub }: { port: AgentPort; hub: Parameters<typeof PeerProjects>[0]["hub"] }) {
  return <PeerProjects port={port} hub={hub} settings={usePeersSettings(port)} />;
}

const settings = (over: Partial<PeersSettings> = {}): PeersSettings => ({
  enabled: true, dispatch: false, release_projects: [], max_dispatch_depth: 1, max_dispatched_sessions: 8, open_nested: false, path: "/home/me/.reasonix/config.toml", applies: "next_turn", pending: false, available: true, ...over,
});
const ws = (root: string, name: string, remembered = true): TreeWorkspace => ({ root, name, remembered, sessions: [] });

function draw(initial: PeersSettings, tree: TreeWorkspace[]) {
  const port = new MockPort();
  let current = initial;
  vi.spyOn(port, "peersSettings").mockImplementation(async () => current);
  const save = vi.spyOn(port, "savePeersSettings").mockImplementation(async (edit) => {
    const keep = edit.release_projects;
    current = { ...current, release_projects: keep ? keep.map((path) => ({ path, name: path.split("/").at(-1)! })) : current.release_projects, pending: true };
    return current;
  });
  const hub = { tree: vi.fn(async () => tree) };
  render(<Projects port={port as unknown as AgentPort} hub={hub} />);
  return { save, hub, setCurrent: (next: PeersSettings) => { current = next; } };
}

describe("the projects whose sessions take a message without asking", () => {
  it("says so when there are none, and that every message asks first", async () => {
    draw(settings(), []);
    expect(await screen.findByText(/还没有放行任何项目/)).toBeTruthy();
    expect(screen.queryByRole("list", { name: "已长期放行的项目" })).toBeNull();
  });

  it("lists each project allowed through with a way to take it back", async () => {
    const { save } = draw(settings({ release_projects: [{ path: "/work/site", name: "site" }, { path: "/work/api", name: "api" }] }), []);
    const list = await screen.findByRole("list", { name: "已长期放行的项目" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(within(list).getByText("/work/site")).toBeTruthy();
    await userEvent.click(within(within(list).getAllByRole("listitem")[0]).getByRole("button", { name: /撤销/ }));
    await waitFor(() => expect(save).toHaveBeenCalledExactlyOnceWith({ release_projects: ["/work/api"] }));
  });

  it("offers only projects the person has added, and not the ones already allowed through", async () => {
    draw(settings({ release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/site", "site"), ws("/work/api", "api"), ws("/launch", "launch", false), ws("/work/docs", "docs")]);
    const pick = await screen.findByRole("combobox", { name: "放行这个项目" });
    expect([...pick.querySelectorAll("option")].map((o) => o.textContent)).toEqual(["api — /work/api", "docs — /work/docs"]);
  });

  it("allows the chosen project through, adding it to the list and saying when it takes effect", async () => {
    const { save } = draw(settings({ release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/site", "site"), ws("/work/api", "api"), ws("/work/docs", "docs")]);
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "放行这个项目" }), "/work/docs");
    await userEvent.click(screen.getByRole("button", { name: "放行" }));
    await waitFor(() => expect(save).toHaveBeenCalledExactlyOnceWith({ release_projects: ["/work/site", "/work/docs"] }));
    expect(await screen.findByText(/下个回合起生效/)).toBeTruthy();
    expect(within(screen.getByRole("list", { name: "已长期放行的项目" })).getByText("/work/docs")).toBeTruthy();
  });

  it("reads the list again before writing it, so another window's change is not undone", async () => {
    const { save, setCurrent } = draw(settings({ release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/api", "api")]);
    await screen.findByRole("combobox", { name: "放行这个项目" });
    setCurrent(settings({ release_projects: [{ path: "/work/site", name: "site" }, { path: "/work/other", name: "other" }] }));
    await userEvent.click(screen.getByRole("button", { name: "放行" }));
    await waitFor(() => expect(save).toHaveBeenCalledExactlyOnceWith({ release_projects: ["/work/site", "/work/other", "/work/api"] }));
  });

  it("offers nothing to add before messages are on, but still takes a project back", async () => {
    const { save } = draw(settings({ enabled: false, release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/api", "api")]);
    expect(await screen.findByText(/先启用会话间消息/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "放行" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /撤销放行/ }));
    await waitFor(() => expect(save).toHaveBeenCalledExactlyOnceWith({ release_projects: [] }));
  });

  it("says there is nothing to add when every added project is already allowed through", async () => {
    draw(settings({ release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/site", "site")]);
    expect(await screen.findByText(/没有可以放行的项目/)).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "放行这个项目" })).toBeNull();
  });

  it("holds the controls inert where the session has no folder to open", async () => {
    draw(settings({ available: false, release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/api", "api")]);
    expect(await screen.findByText(/此会话不支持/)).toBeTruthy();
    expect((screen.getByRole("button", { name: /撤销放行/ }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "放行" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows a refusal beside the list and keeps the list as it was", async () => {
    const { save } = draw(settings({ release_projects: [{ path: "/work/site", name: "site" }] }), [ws("/work/api", "api")]);
    save.mockRejectedValue(new HttpError(500, "x", { code: "peer.settings_unwritable", params: { detail: "/home/me/.reasonix/config.toml" } }));
    await userEvent.click(await screen.findByRole("button", { name: "放行" }));
    expect((await screen.findByRole("alert")).textContent).toContain("没能保存");
    expect(within(screen.getByRole("list", { name: "已长期放行的项目" })).getAllByRole("listitem")).toHaveLength(1);
  });
});
