// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UserCard } from "./cards/UserCard";
import { ToolCard } from "./cards/ToolCard";
import { openCards } from "./cards/openCards";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, SessionStatus } from "../port/port";

Element.prototype.getAnimations ??= () => [];

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const path = ".reasonix/attachments/preview.png";
const src = `/rt/picture/workspace/image?path=${encodeURIComponent(path)}`;
const item = { t: "user" as const, id: "picture", text: `@${path} Read this`, attachments: [{ path }] };
const shot = "data:image/png;base64,aW1hZ2U=";

describe("shared image preview entrances", () => {
  it("opens a stored user image on double click, keeps its original URL, and restores focus", async () => {
    const user = userEvent.setup();
    const { container } = render(<UserCard item={item} port={{ workspaceImageURL: () => src }} />);
    const trigger = screen.getByRole("button", { name: "查看图片 preview.png" });
    await user.dblClick(trigger);
    const dialog = screen.getByRole("dialog", { name: "preview.png" });
    fireEvent.click(dialog, { detail: 2 });
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(container.contains(dialog)).toBe(false);
    expect(within(dialog).getByRole("img").getAttribute("src")).toBe(src);
    const close = within(dialog).getByRole("button", { name: "关闭图片预览" });
    expect(document.activeElement).toBe(close);
    await user.tab();
    expect(document.activeElement).toBe(close);
    await user.tab({ shift: true });
    expect(document.activeElement).toBe(close);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
    expect(screen.getByText("Read this")).toBeTruthy();
  });

  it("opens from the keyboard and keeps clicks on the image inside the viewer", async () => {
    const user = userEvent.setup();
    render(<UserCard item={item} port={{ workspaceImageURL: () => src }} />);
    screen.getByRole("button", { name: "查看图片 preview.png" }).focus();
    await user.keyboard("{Enter}");
    await user.click(within(screen.getByRole("dialog")).getByRole("img"));
    expect(screen.getByRole("dialog")).toBeTruthy();
    await user.click(screen.getByRole("dialog"));
    expect(screen.queryByRole("dialog")).toBeNull();
    await user.keyboard(" ");
    expect(screen.getByRole("dialog")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "关闭图片预览" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("releases the scroll lock and closes when a message or its runtime URL changes", () => {
    document.body.style.overflow = "auto";
    const view = render(<UserCard item={item} port={{ workspaceImageURL: () => src }} />);
    fireEvent.click(screen.getByRole("button", { name: "查看图片 preview.png" }));
    expect(document.body.style.overflow).toBe("hidden");
    view.rerender(<UserCard item={item} port={{ workspaceImageURL: () => "/rt/new/image" }} />);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.body.style.overflow).toBe("auto");
    fireEvent.click(screen.getByRole("button", { name: "查看图片 preview.png" }));
    expect(within(screen.getByRole("dialog")).getByRole("img").getAttribute("src")).toBe("/rt/new/image");
    view.unmount();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.body.style.overflow).toBe("auto");
    document.body.style.overflow = "";
  });

  it("keeps a failed preview closable and makes a missing thumbnail unavailable", () => {
    render(<UserCard item={item} port={{ workspaceImageURL: () => src }} />);
    fireEvent.click(screen.getByRole("button", { name: "查看图片 preview.png" }));
    fireEvent.error(within(screen.getByRole("dialog")).getByRole("img"));
    expect(within(screen.getByRole("dialog")).getByText("图片不可用")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "关闭图片预览" }));
    fireEvent.error(screen.getByRole("img"));
    expect(screen.queryByRole("button", { name: "查看图片 preview.png" })).toBeNull();
    expect(screen.getByText("图片不可用")).toBeTruthy();
    expect(screen.getByText("Read this")).toBeTruthy();
  });

  it("uses the same viewer for tool screenshots and preserves the expanded tool disclosure", async () => {
    const user = userEvent.setup();
    const { container } = render(<ToolCard tool={{ id: "shot", name: "computer_read", images: [shot], output: "screen", readOnly: true }} running={false} />);
    const disclosure = container.querySelector("details")!;
    openCards(container);
    await user.click(screen.getByRole("button", { name: "查看图片 工具截图 1" }));
    expect(within(screen.getByRole("dialog")).getByRole("img").getAttribute("src")).toBe(shot);
    expect(disclosure.open).toBe(true);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(disclosure.open).toBe(true);
  });

  it.each(["upload", "paste"])("previews a composer image from %s without sending, and releases the viewer when the attachment is removed", async (source) => {
    const user = userEvent.setup();
    const port = new MockPort();
    vi.spyOn(port, "attach").mockResolvedValue({ path, ref: `@${path}`, image: true });
    const submit = vi.fn(async () => true);
    const view = render(<Composer port={port as AgentPort} status={{ modelRef: "deepseek/m" } as SessionStatus} running={false} onSubmit={submit} onChanged={vi.fn()} onError={vi.fn()} />);
    const file = new File(["image"], "preview.png", { type: "image/png" });
    if (source === "upload") fireEvent.change(view.container.querySelector('input[type="file"]')!, { target: { files: [file] } });
    else fireEvent.paste(screen.getByRole("combobox", { name: "任务输入" }), { clipboardData: { files: [file], getData: () => "" } });
    const trigger = await screen.findByRole("button", { name: "查看图片 preview.png" });
    await waitFor(() => expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false));
    await user.click(trigger);
    expect(within(screen.getByRole("dialog")).getByRole("img").getAttribute("src")).toMatch(/^blob:/);
    await user.keyboard("{Escape}");
    expect(submit).not.toHaveBeenCalled();
    await user.click(trigger);
    fireEvent.click(screen.getByRole("button", { name: "移除 preview.png" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.body.style.overflow).not.toBe("hidden");
    expect(submit).not.toHaveBeenCalled();
  });
});
