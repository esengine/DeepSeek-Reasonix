// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../state/session";
import type { AgentPort, SessionStatus } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { fromHistory } from "../state/session";
import { MockPort } from "../port/mock";
import { UserCard } from "./cards/UserCard";
import { Pane } from "./Pane";
import { Composer } from "./Composer";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const path = ".reasonix/attachments/clipboard-20261008-002000.123456-000001.png";
const text = `@${path} 请看这张图片`;
const item = { t: "user", id: "picture", text } as Extract<Item, { t: "user" }>;
const port = new MockPort();

describe("saved images in user messages", () => {
  it.each(["paste", "upload"])("previews the token returned by %s after it is submitted", async (source) => {
    const upload = vi.spyOn(port, "attach").mockResolvedValue({ path, ref: `@${path}`, image: true });
    const submit = vi.fn(async (_line: string) => true);
    const view = render(<Composer port={port as AgentPort} status={{ modelRef: "deepseek/m" } as SessionStatus} running={false} onSubmit={submit} onChanged={vi.fn()} onError={vi.fn()} />);
    const box = screen.getByRole("combobox", { name: "任务输入" });
    const file = new File(["fixture"], "paste.png", { type: "image/png" });
    if (source === "paste") fireEvent.paste(box, { clipboardData: { files: [file], getData: () => "" } });
    else fireEvent.change(view.container.querySelector('input[type="file"]')!, { target: { files: [file] } });
    await waitFor(() => expect(upload).toHaveBeenCalled());
    await waitFor(() => expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(submit).toHaveBeenCalledWith(`@${path}`));
    view.unmount();
    render(<UserCard item={{ ...item, text: submit.mock.calls[0][0] }} port={port} />);
    expect(screen.getByRole("img").getAttribute("src")).toBe(port.workspaceImageURL(path));
  });

  it("connects a restored message to its own pane's image endpoint", async () => {
    const saved = new MockPort();
    vi.spyOn(saved, "history").mockResolvedValue([{ role: "user", content: text }]);
    const url = vi.spyOn(saved, "workspaceImageURL").mockImplementation((path) => `/runtime/picture?path=${encodeURIComponent(path)}`);
    render(<Pane port={saved as AgentPort} rt={{ id: "p1", root: "/w", name: "w" } as RuntimeView} title="w" active visible sideHost={null} side={false}
      onFocus={vi.fn()} onReport={vi.fn()} onSessionChanged={vi.fn()} pulse={0} findPulse={0}
      onSettings={vi.fn()} needsProject={false} onOpenProject={vi.fn()} onKeepHere={vi.fn()}
      theme="dark" dockW={560} dockMax={880} onDockW={vi.fn()} />);
    const image = await screen.findByRole("img", { name: path.split("/").at(-1) });
    expect(image.getAttribute("src")).toBe(`/runtime/picture?path=${encodeURIComponent(path)}`);
    expect(url).toHaveBeenCalledWith(path);
  });

  it("shows a saved image through the pane's workspace endpoint and keeps the reference", () => {
    render(<UserCard item={item} port={port} />);
    const image = screen.getByRole("img", { name: path.split("/").at(-1) });
    expect(image.getAttribute("src")).toBe(port.workspaceImageURL(path));
    expect(image.getAttribute("loading")).toBe("lazy");
    expect(screen.getByText(text)).toBeTruthy();
  });

  it("shows the same attachment after history is restored", () => {
    const restored = fromHistory([{ role: "user", content: text }]).items[0] as typeof item;
    render(<UserCard item={restored} port={port} />);
    expect(screen.getByRole("img").getAttribute("src")).toBe(port.workspaceImageURL(path));
  });

  it("shows each saved image once without treating other references as pictures", () => {
    const other = path.replace("000001.png", "000002.webp");
    render(<UserCard item={{ ...item, text: `@${path} @${other}\n@${path} @.reasonix/attachments/clipboard-note.txt @photo.png` }} port={port} />);
    expect(screen.getAllByRole("img").map((image) => image.getAttribute("src"))).toEqual([port.workspaceImageURL(path), port.workspaceImageURL(other)]);
  });

  it.each([
    "someone@.reasonix/attachments/clipboard-1.png",
    "@.reasonix/attachments/../private.png",
    "@.reasonix/attachments/clipboard-1.png.exe",
    "@https://example.com/photo.png",
    "ordinary text",
  ])("leaves unrelated text alone: %s", (line) => {
    render(<UserCard item={{ ...item, text: line }} port={port} />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText(line)).toBeTruthy();
  });

  it("keeps the preview frame and reference readable if its file is missing", async () => {
    render(<UserCard item={item} port={port} />);
    const image = screen.getByRole("img");
    const frame = image.parentElement;
    fireEvent.error(image);
    await waitFor(() => expect(screen.queryByRole("img")).toBeNull());
    expect(screen.getByText("图片不可用").parentElement).toBe(frame);
    expect(frame?.isConnected).toBe(true);
    expect(screen.getByText(text)).toBeTruthy();
  });

  it("keeps another attachment visible when one image fails", () => {
    const other = path.replace("000001.png", "000002.webp");
    render(<UserCard item={{ ...item, text: `@${path} @${other}` }} port={port} />);
    fireEvent.error(screen.getAllByRole("img")[0]);
    expect(screen.getByText("图片不可用")).toBeTruthy();
    expect(screen.getByRole("img").getAttribute("src")).toBe(port.workspaceImageURL(other));
  });

  it("retries the image when its runtime endpoint changes", () => {
    const view = render(<UserCard item={item} port={port} />);
    fireEvent.error(screen.getByRole("img"));
    expect(screen.getByText("图片不可用")).toBeTruthy();
    view.rerender(<UserCard item={item} port={{ workspaceImageURL: () => "/rt/other/image" }} />);
    expect(screen.queryByText("图片不可用")).toBeNull();
    expect(screen.getByRole("img").getAttribute("src")).toBe("/rt/other/image");
  });

  it("copies and edits the original message including its attachment token", async () => {
    const user = userEvent.setup();
    const write = vi.spyOn(navigator.clipboard, "writeText");
    render(<UserCard item={item} port={port} cp={{ turn: 1, prompt: text, files: 0, msgIndex: 1 }} onResend={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "复制" }));
    expect(write).toHaveBeenCalledExactlyOnceWith(text);
    await user.click(screen.getByRole("button", { name: "改写" }));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(text);
    expect(screen.queryByRole("img")).toBeNull();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByRole("img")).toBeTruthy();
    write.mockRestore();
  });
});
