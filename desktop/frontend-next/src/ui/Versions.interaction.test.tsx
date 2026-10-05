// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Versions } from "./Versions";
import { HttpError, type UpdateProgress, type VersionHub } from "../port/port";

afterEach(cleanup);

const hub: VersionHub = {
  current: "2.20.0",
  pinned: "",
  stalePin: false,
  latest: "2.21.0",
  newer: true,
  versions: [
    { version: "2.21.0", tag: "studio-v2.21.0", publishedAt: "", current: false, older: false },
    { version: "2.20.0", tag: "studio-v2.20.0", publishedAt: "", current: true, older: false },
  ],
};

function portAt(progress: UpdateProgress, restart = vi.fn((_v: string, _force: boolean) => Promise.resolve())) {
  return {
    versions: () => Promise.resolve(hub),
    pinVersion: vi.fn(() => Promise.resolve()),
    goToVersion: vi.fn(() => Promise.resolve()),
    restartToVersion: restart,
    onUpdateProgress: (cb: (p: UpdateProgress) => void) => {
      cb(progress);
      return () => {};
    },
  };
}

describe("the version panel", () => {
  it("waits for the person before restarting into a downloaded release", async () => {
    const port = portAt({ version: "2.21.0", phase: "ready", received: 0, total: 0 });
    render(<Versions port={port} />);
    expect(await screen.findByText("2.21.0 已下载并通过签名校验")).toBeTruthy();
    expect(port.restartToVersion).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    expect(port.restartToVersion).toHaveBeenCalledWith("2.21.0", false);
  });

  it("names running work and restarts over it only when asked again", async () => {
    const restart = vi.fn((_v: string, force: boolean) =>
      force ? Promise.resolve() : Promise.reject(new HttpError(409, "busy", { code: "update.restart_busy", params: { n: 2 } }, true)),
    );
    const port = portAt({ version: "2.21.0", phase: "ready", received: 0, total: 0 }, restart);
    render(<Versions port={port} />);
    await userEvent.click(await screen.findByRole("button", { name: "立即重启" }));
    expect(await screen.findByText("有 2 项任务正在运行")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "仍然重启" }));
    expect(restart).toHaveBeenLastCalledWith("2.21.0", true);
  });

  it("keeps a postponed release one click away on its row", async () => {
    const port = portAt({ version: "2.21.0", phase: "ready", received: 0, total: 0 });
    render(<Versions port={port} />);
    await userEvent.click(await screen.findByRole("button", { name: "稍后" }));
    expect(screen.queryByText("2.21.0 已下载并通过签名校验")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "重启以完成安装" }));
    expect(port.restartToVersion).toHaveBeenCalledWith("2.21.0", false);
  });

  it("says a move is fetching the full package, and why, when the delta was given up", async () => {
    const port = portAt({ version: "2.21.0", phase: "downloading", received: 1024, total: 4096, delta_skipped: "update.delta.not_swappable" });
    render(<Versions port={port} />);
    const line = await screen.findByText(/^下载完整安装包/);
    expect(line.getAttribute("title")).toBe("安装目录无法就地替换文件，本次改为下载完整安装包。");
    const described = document.getElementById(line.getAttribute("aria-describedby") ?? "");
    expect(described?.textContent).toBe("安装目录无法就地替换文件，本次改为下载完整安装包。");
  });

  it("keeps the plain download line when no delta was given up", async () => {
    const port = portAt({ version: "2.21.0", phase: "downloading", received: 1024, total: 4096 });
    render(<Versions port={port} />);
    const line = await screen.findByText(/^下载中/);
    expect(line.getAttribute("title")).toBeNull();
    expect(line.getAttribute("aria-describedby")).toBeNull();
  });

  it("explains a move that did not land by its code, with the way out", async () => {
    const port = portAt({ version: "2.21.0", phase: "error", received: 0, total: 0, code: "update.not_applied", err: "update: set aside app.exe: access denied" });
    render(<Versions port={port} />);
    expect(await screen.findByText("上次更新到 2.21.0 没有生效")).toBeTruthy();
    const link = screen.getByRole("link", { name: "下载完整安装包" });
    expect(link.getAttribute("href")).toBe("https://github.com/esengine/DeepSeek-Reasonix/releases/tag/studio-v2.21.0");
    expect(screen.getByText("update: set aside app.exe: access denied")).toBeTruthy();
  });
});
