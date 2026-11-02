// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../state/session";
import type { AgentPort, SessionStatus } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { fromHistory, initialState } from "../state/session";
import { nameTurnStart, othersSteer } from "../state/turn_start";
import type { WireEvent } from "../port/wire";
import { MockPort } from "../port/mock";
import { UserCard } from "./cards/UserCard";
import { Pane } from "./Pane";
import { Composer } from "./Composer";
import { CLIP_MANIFEST, readClipText } from "./clipboard";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const path = ".reasonix/attachments/clipboard-20261008-002000.123456-000001.png";
const text = `@${path} 请看这张图片`;
const stored = (p: string) => ({ path: p });
const item = { t: "user", id: "picture", text, attachments: [stored(path)] } as Extract<Item, { t: "user" }>;
const port = new MockPort();

describe("saved images in user messages", () => {
  it.each(["paste", "upload"])("sends the token returned by %s and previews what the kernel names for the turn", async (source) => {
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
    await waitFor(() => expect(submit).toHaveBeenCalled());
    expect((submit.mock.calls[0] as unknown[])[0]).toBe(`@${path}`);
    view.unmount();
    const named = nameTurnStart(
      { ...initialState, items: [{ t: "user", id: "me", text: submit.mock.calls[0][0] }], awaitingTurnStart: ["me"] },
      { kind: "turn_started", authoredTurn: 1, msgIndex: 1, attachments: [stored(path)] } as WireEvent,
    );
    render(<UserCard item={named.items[0] as typeof item} port={port} />);
    expect(screen.getByRole("img").getAttribute("src")).toBe(port.workspaceImageURL(path));
  });

  it("does not preview text that merely spells the reference", () => {
    render(<UserCard item={{ ...item, attachments: undefined }} port={port} />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText(text)).toBeTruthy();
  });

  it("previews what a steer or another client's turn was told it carries", () => {
    const steer = othersSteer([], { kind: "steer", text, itemId: "s1", attachments: [stored(path)] } as WireEvent)[0] as typeof item;
    render(<UserCard item={steer} port={port} />);
    expect(screen.getAllByRole("img")).toHaveLength(1);
    cleanup();
    const theirs = nameTurnStart({ ...initialState, items: [] }, { kind: "turn_started", text, authoredTurn: 1, msgIndex: 1, attachments: [stored(path)] } as WireEvent);
    render(<UserCard item={theirs.items[0] as typeof item} port={port} />);
    expect(screen.getAllByRole("img")).toHaveLength(1);
  });

  it("connects a restored message to its own pane's image endpoint", async () => {
    const saved = new MockPort();
    vi.spyOn(saved, "history").mockResolvedValue([{ role: "user", content: text, attachments: [stored(path)] }]);
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
    const restored = fromHistory([{ role: "user", content: text, attachments: [stored(path)] }]).items[0] as typeof item;
    render(<UserCard item={restored} port={port} />);
    expect(screen.getByRole("img").getAttribute("src")).toBe(port.workspaceImageURL(path));
  });

  it("shows each stored image once", () => {
    const other = path.replace("000001.png", "000002.webp");
    render(<UserCard item={{ ...item, text: `@${path} @${other}`, attachments: [stored(path), stored(other), stored(path)] }} port={port} />);
    expect(screen.getAllByRole("img").map((image) => image.getAttribute("src"))).toEqual([port.workspaceImageURL(path), port.workspaceImageURL(other)]);
  });

  it("previews a queued line only once its turn starts, and never for a turn that never started", () => {
    const queued = { t: "user", id: "q", text, pending: true, queued: "followup" } as Extract<Item, { t: "user" }>;
    const view = render(<UserCard item={queued} port={port} />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText(text)).toBeTruthy();
    const started = nameTurnStart(
      { ...initialState, items: [queued], awaitingTurnStart: ["q"] },
      { kind: "turn_started", authoredTurn: 1, msgIndex: 1, attachments: [stored(path)] } as WireEvent,
    ).items[0] as typeof item;
    expect(started.pending).toBe(false);
    view.rerender(<UserCard item={started} port={port} />);
    expect(screen.getByRole("img").getAttribute("src")).toBe(port.workspaceImageURL(path));
    const refused = nameTurnStart(
      { ...initialState, items: [queued], awaitingTurnStart: ["q"] },
      { kind: "turn_started", authoredTurn: 2, msgIndex: 2 } as WireEvent,
    ).items[0] as typeof item;
    view.rerender(<UserCard item={refused} port={port} />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText(text)).toBeTruthy();
  });

  it("keeps the preview frame and reference readable if its file is missing", async () => {
    render(<UserCard item={item} port={port} />);
    const image = screen.getByRole("img");
    const frame = image.closest(".user-image");
    fireEvent.error(image);
    await waitFor(() => expect(screen.queryByRole("img")).toBeNull());
    expect(screen.getByText("图片不可用").parentElement).toBe(frame);
    expect(frame?.isConnected).toBe(true);
    expect(screen.getByText(text)).toBeTruthy();
  });

  it("keeps another attachment visible when one image fails", () => {
    const other = path.replace("000001.png", "000002.webp");
    render(<UserCard item={{ ...item, text: `@${path} @${other}`, attachments: [stored(path), stored(other)] }} port={port} />);
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

  it("copies the words with their picture, and rewriting still carries the picture", async () => {
    const user = userEvent.setup();
    const written: { data: Record<string, Blob> }[] = [];
    class Item {
      constructor(readonly data: Record<string, Blob>) {}
    }
    vi.stubGlobal("ClipboardItem", Item);
    const write = vi.fn(async (items: { data: Record<string, Blob> }[]) => { written.push(...items); });
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { write, writeText: vi.fn() } });
    vi.stubGlobal("fetch", async () => new Response(new Blob(["png"], { type: "image/png" }), { headers: { "content-type": "image/png" } }));
    render(<UserCard item={{ ...item, text: "请看这张图片" }} port={port} cp={{ turn: 1, prompt: text, files: 0, msgIndex: 1 }} onResend={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "复制全部" }));
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(readClipText(await written[0].data["text/plain"].text())).toEqual({ text: "请看这张图片", images: [{ path }] });
    await user.click(screen.getByRole("button", { name: "改写" }));
    // The rewrite box carries the line the model reads, which is the only place
    // the picture's token belongs.
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(`请看这张图片 @${path}`);
    expect(screen.queryByRole("img")).toBeNull();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByRole("img")).toBeTruthy();
  });
});

describe("pasting a message that was copied with its pictures", () => {
  const copyAllOf = (name: string, pictures: string[]) => ({
    files: [],
    getData: (type: string) => type === CLIP_MANIFEST ? JSON.stringify({ v: 1, text: name, images: pictures.map((p) => ({ path: p })) }) : "",
  });

  it("puts the picture back as an attachment and leaves only the words in the box", async () => {
    const submit = vi.fn(async () => true);
    render(<Composer port={port as AgentPort} status={{ modelRef: "deepseek/m" } as SessionStatus} running={false} onSubmit={submit} onChanged={vi.fn()} onError={vi.fn()} />);
    const box = screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement;
    fireEvent.paste(box, { clipboardData: copyAllOf("请看这张图", [path]) });
    await screen.findByText(path.split("/").at(-1)!);
    expect(box.value).toBe("请看这张图 ");
    expect(box.value).not.toContain(path);
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(submit).toHaveBeenCalled());
    const [line, call] = submit.mock.calls[0] as unknown as [string, { display?: string }];
    expect(line).toBe(`@${path} 请看这张图`);
    // The row this window draws is the words alone: the kernel stores the same
    // reading, so a reload does not bring the path back.
    expect(call?.display).toBe("请看这张图");
  });

  it("keeps the words and says how many pictures this workspace no longer has", async () => {
    const restore = vi.spyOn(port, "restoreAttachments");
    render(<Composer port={port as AgentPort} status={{ modelRef: "deepseek/m" } as SessionStatus} running={false} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} />);
    const box = screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement;
    fireEvent.paste(box, { clipboardData: copyAllOf("上次的图", ["./old/.reasonix/attachments/gone.png"]) });
    expect(await screen.findByText("1 张图片不在当前工作区，粘贴时已忽略")).toBeTruthy();
    expect(box.value).toBe("上次的图 ");
    expect(box.value).not.toContain(".reasonix/attachments");
    expect(restore).toHaveBeenCalledWith(["./old/.reasonix/attachments/gone.png"]);
  });

  it("does not restore anything from a text-only copy", async () => {
    const restore = vi.spyOn(port, "restoreAttachments");
    render(<Composer port={port as AgentPort} status={{ modelRef: "deepseek/m" } as SessionStatus} running={false} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} />);
    const box = screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement;
    const notHijacked = fireEvent.paste(box, { clipboardData: { files: [], getData: (type: string) => type === "text/plain" ? "只有文字" : "" } });
    // A short plain paste is still the browser's: the handler only watches for
    // the manifest a "copy all" writes.
    expect(notHijacked).toBe(true);
    expect(box.value).toBe("");
    expect(restore).not.toHaveBeenCalled();
  });
});
