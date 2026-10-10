// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Versions } from "./Versions";
import { HttpError, type UpdateProgress, type VersionHub, type VersionNotes } from "../port/port";

afterEach(cleanup);

const row = (version: string, hasNotes: boolean, current = false, older = false) => ({
  version,
  tag: `studio-v${version}`,
  publishedAt: "",
  hasNotes,
  current,
  older,
});

const hub: VersionHub = {
  current: "2.20.0",
  pinned: "",
  stalePin: false,
  latest: "2.21.0",
  newer: true,
  versions: [row("2.21.0", true), row("2.20.0", true, true), row("2.19.0", false, false, true)],
};

type Notes = (v: string, retry?: boolean) => Promise<VersionNotes>;

function portWith(versionNotes: Notes, h: VersionHub = hub) {
  return {
    versions: () => Promise.resolve(h),
    pinVersion: vi.fn(() => Promise.resolve()),
    goToVersion: vi.fn(() => Promise.resolve()),
    restartToVersion: vi.fn(() => Promise.resolve()),
    onUpdateProgress: (_cb: (p: UpdateProgress) => void) => () => {},
    versionNotes: vi.fn(versionNotes),
  };
}

const doc = (version: string, markdown: string): Notes => () => Promise.resolve({ version, markdown, cached: false });
const refused = (code: string) => new HttpError(502, "english fallback for logs", { code }, true);

describe("version notes on the version panel", () => {
  it("offers the view only on rows whose catalog entry has notes, and asks for nothing until it is used", async () => {
    const port = portWith(doc("2.21.0", "# hi"));
    render(<Versions port={port} />);
    await screen.findByText("2.19.0");
    expect(screen.getAllByRole("button", { name: "更新内容" })).toHaveLength(2);
    expect(port.versionNotes).not.toHaveBeenCalled();
  });

  it("shows no control and makes no request when the catalog names no notes at all", async () => {
    const bare: VersionHub = { ...hub, versions: hub.versions.map((v) => ({ ...v, hasNotes: false })) };
    const port = portWith(doc("2.21.0", "x"), bare);
    render(<Versions port={port} />);
    await screen.findByText("2.19.0");
    expect(screen.queryByRole("button", { name: "更新内容" })).toBeNull();
    expect(port.versionNotes).not.toHaveBeenCalled();
  });

  it("tolerates a kernel that does not send hasNotes", async () => {
    const old = { ...hub, versions: hub.versions.map(({ hasNotes: _h, ...rest }) => rest) } as unknown as VersionHub;
    render(<Versions port={portWith(doc("2.21.0", "x"), old)} />);
    await screen.findByText("2.19.0");
    expect(screen.queryByRole("button", { name: "更新内容" })).toBeNull();
  });

  it("expands inline with the document, and names the state for assistive tech", async () => {
    const port = portWith(doc("2.21.0", "# 本版新增\n\n- 修好了一件事"));
    render(<Versions port={port} />);
    const [button] = await screen.findAllByRole("button", { name: "更新内容" });
    expect(button.getAttribute("aria-expanded")).toBe("false");
    await userEvent.click(button);
    expect(await screen.findByText("修好了一件事")).toBeTruthy();
    const open = screen.getByRole("button", { name: "收起更新内容" });
    expect(open.getAttribute("aria-expanded")).toBe("true");
    expect(document.getElementById(open.getAttribute("aria-controls") ?? "")).toBeTruthy();
    expect(screen.getByRole("region", { name: "2.21.0 的更新内容" })).toBeTruthy();
    expect(port.versionNotes).toHaveBeenCalledWith("2.21.0", false);
  });

  it("collapses on the button and on Escape, which hands focus back and does not travel on", async () => {
    const port = portWith(doc("2.21.0", "- 一条"));
    const onWindowKey = vi.fn();
    window.addEventListener("keydown", onWindowKey);
    render(<Versions port={port} />);
    const [button] = await screen.findAllByRole("button", { name: "更新内容" });
    await userEvent.click(button);
    await screen.findByText("一条");
    await userEvent.click(screen.getByRole("button", { name: "收起更新内容" }));
    expect(screen.queryByText("一条")).toBeNull();
    await userEvent.click(screen.getAllByRole("button", { name: "更新内容" })[0]);
    await screen.findByText("一条");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByText("一条")).toBeNull();
    expect(document.activeElement).toBe(screen.getAllByRole("button", { name: "更新内容" })[0]);
    expect(onWindowKey).not.toHaveBeenCalled();
    window.removeEventListener("keydown", onWindowKey);
  });

  it("closes on Escape pressed inside the document too, and returns focus to the control", async () => {
    const port = portWith(() => Promise.reject(refused("studio.notes_unreachable")));
    render(<Versions port={port} />);
    await userEvent.click((await screen.findAllByRole("button", { name: "更新内容" }))[0]);
    const link = await screen.findByRole("link", { name: "在 GitHub 查看" });
    link.focus();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("region")).toBeNull();
    expect(document.activeElement).toBe(screen.getAllByRole("button", { name: "更新内容" })[0]);
  });

  it("opens one at a time", async () => {
    const port = portWith((v) => Promise.resolve({ version: v, markdown: `- 内容 ${v}`, cached: false }));
    render(<Versions port={port} />);
    const buttons = await screen.findAllByRole("button", { name: "更新内容" });
    await userEvent.click(buttons[0]);
    await screen.findByText("内容 2.21.0");
    await userEvent.click(screen.getAllByRole("button", { name: "更新内容" })[0]);
    await screen.findByText("内容 2.20.0");
    expect(screen.queryByText("内容 2.21.0")).toBeNull();
    expect(screen.getAllByRole("region")).toHaveLength(1);
  });

  it("does not ask again for a document it already has", async () => {
    const port = portWith(doc("2.21.0", "- 一条"));
    render(<Versions port={port} />);
    const [button] = await screen.findAllByRole("button", { name: "更新内容" });
    await userEvent.click(button);
    await screen.findByText("一条");
    await userEvent.click(screen.getByRole("button", { name: "收起更新内容" }));
    await userEvent.click(screen.getAllByRole("button", { name: "更新内容" })[0]);
    await screen.findByText("一条");
    expect(port.versionNotes).toHaveBeenCalledTimes(1);
  });

  it("says why it failed, keeps the GitHub page one click away, and retries past the failure", async () => {
    let fail = true;
    const port = portWith(() => (fail ? Promise.reject(refused("studio.notes_unreachable")) : Promise.resolve({ version: "2.21.0", markdown: "- 好了", cached: false })));
    render(<Versions port={port} />);
    await userEvent.click((await screen.findAllByRole("button", { name: "更新内容" }))[0]);
    expect(await screen.findByText("暂时取不到更新内容，请检查网络后重试")).toBeTruthy();
    const link = screen.getByRole("link", { name: "在 GitHub 查看" });
    expect(link.getAttribute("href")).toBe("https://github.com/esengine/DeepSeek-Reasonix/releases/tag/studio-v2.21.0");
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText("好了")).toBeTruthy();
    expect(port.versionNotes).toHaveBeenLastCalledWith("2.21.0", true);
  });

  it("offers no retry for a release the mirror has no notes for", async () => {
    const port = portWith(() => Promise.reject(new HttpError(404, "x", { code: "studio.notes_absent" }, true)));
    render(<Versions port={port} />);
    await userEvent.click((await screen.findAllByRole("button", { name: "更新内容" }))[0]);
    expect(await screen.findByText("这个版本没有发布更新内容")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
    expect(screen.getByRole("link", { name: "在 GitHub 查看" })).toBeTruthy();
  });

  it("draws no picture and runs no markup that the document carries", async () => {
    const hostile = "![track](https://evil.test/p.png)\n\n<img src=\"https://evil.test/q.png\"><script>window.__ran = 1</script>\n\n[链接](https://example.com)";
    const port = portWith(doc("2.21.0", hostile));
    render(<Versions port={port} />);
    await userEvent.click((await screen.findAllByRole("button", { name: "更新内容" }))[0]);
    await screen.findByText("链接");
    await waitFor(() => expect(document.querySelector(".vnotes img")).toBeNull());
    expect(document.querySelector(".vnotes script")).toBeNull();
    expect((window as unknown as { __ran?: number }).__ran).toBeUndefined();
    expect(screen.getByText("链接").closest("a")?.getAttribute("href")).toBe("https://example.com");
  });

  it("keeps the install and pin actions beside the notes control", async () => {
    const port = portWith(doc("2.21.0", "- x"));
    render(<Versions port={port} />);
    await screen.findAllByRole("button", { name: "更新内容" });
    expect(screen.getByRole("button", { name: "安装这个版本" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "固定在这里" })).toBeTruthy();
  });
});
