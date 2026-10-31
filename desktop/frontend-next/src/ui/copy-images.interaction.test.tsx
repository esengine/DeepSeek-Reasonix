// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../state/session";
import { MockPort } from "../port/mock";
import type { AgentPort, SessionStatus } from "../port/port";
import { UserCard } from "./cards/UserCard";
import { Composer } from "./Composer";
import { annotateImagePath, parseImagePaths } from "./imagepaths";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const path = ".reasonix/attachments/clipboard-20261026-190628.194396-000007.png";
const text = "请为这张图生成文案";
const item = { t: "user", id: "picture", text, attachments: [{ path }] } as Extract<Item, { t: "user" }>;

// The clipboard is installed here rather than borrowed from the environment:
// jsdom has none of its own, and a test that reads one only when another
// library has already put it there is a test that depends on the order.
let clipboard: PropertyDescriptor | undefined;
const installClipboard = (write: (value: string) => Promise<void>) => {
  clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: vi.fn(write) } });
  return (navigator.clipboard.writeText as ReturnType<typeof vi.fn>);
};

afterEach(() => {
  if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
  else Reflect.deleteProperty(navigator, "clipboard");
  clipboard = undefined;
});

// What the host answers for an image the window asks to read.
const imageResponse = (ok = true, status = 200, type = "image/png") => ({
  ok,
  status,
  headers: { get: () => type },
  blob: async () => new Blob(["bytes"], { type }),
});

const servesImages = () => vi.stubGlobal("fetch", vi.fn(async () => imageResponse()));

describe("the shape a copied path is written in", () => {
  it("writes a working-folder path and a given path alike, and reads both back", () => {
    expect(annotateImagePath("a.jpg")).toBe("@//a.jpg/");
    expect(annotateImagePath("c:\\a.jpg")).toBe("@//c/:\\a.jpg");
    expect(parseImagePaths("@//a.jpg/ 请看").paths).toEqual(["a.jpg"]);
    expect(parseImagePaths("@//c/:\\a.jpg 请看").paths).toEqual(["c:\\a.jpg"]);
    expect(parseImagePaths("@//a.jpg/ 请看").text).toBe("请看");
  });
});

describe("copying a message that carries images", () => {
  it("copies the words with the image path when the button itself is pressed", async () => {
    servesImages();
    const write = installClipboard(async () => {});
    render(<UserCard item={item} port={new MockPort()} />);
    fireEvent.click(screen.getByRole("button", { name: "复制" }));
    await waitFor(() => expect(write).toHaveBeenCalledWith(`@//${path}/ ${text}`));
  });

  it("offers both copies from the arrow, and closes the menu once one is taken", async () => {
    servesImages();
    const write = installClipboard(async () => {});
    render(<UserCard item={item} port={new MockPort()} />);
    const arrow = screen.getByRole("button", { name: "更多复制选项" });

    fireEvent.click(arrow);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.click(screen.getByRole("menuitem", { name: /仅复制文字/ }));
    await waitFor(() => expect(write).toHaveBeenCalledWith(text));
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.click(arrow);
    fireEvent.click(screen.getByRole("menuitem", { name: /全部复制/ }));
    await waitFor(() => expect(write).toHaveBeenCalledWith(`@//${path}/ ${text}`));
  });

  it("closes the menu on Escape and on a press outside it", async () => {
    servesImages();
    render(<UserCard item={item} port={new MockPort()} />);
    const arrow = screen.getByRole("button", { name: "更多复制选项" });

    fireEvent.click(arrow);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

    fireEvent.click(arrow);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  });

  it("moves through the menu with the keyboard and takes a row with Enter", async () => {
    servesImages();
    const user = userEvent.setup();
    const write = installClipboard(async () => {});
    render(<UserCard item={item} port={new MockPort()} />);
    fireEvent.click(screen.getByRole("button", { name: "更多复制选项" }));

    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /全部复制/ })));
    await user.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /仅复制文字/ }));
    await user.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledWith(text));
  });

  it("falls back to the words when an image cannot be read, and says why", async () => {
    servesImages();
    vi.stubGlobal("fetch", vi.fn(async () => imageResponse(false, 404, "text/plain")));
    const write = installClipboard(async () => {});
    render(<UserCard item={item} port={new MockPort()} />);
    fireEvent.click(screen.getByRole("button", { name: "复制" }));
    await waitFor(() => expect(write).toHaveBeenCalledWith(text));
    expect(await screen.findByText(/图片读取失败/)).toBeTruthy();
  });

  it("says so when the clipboard refuses the write, rather than failing quietly", async () => {
    servesImages();
    installClipboard(async () => { throw new Error("denied"); });
    render(<UserCard item={item} port={new MockPort()} />);
    fireEvent.click(screen.getByRole("button", { name: "复制" }));
    expect(await screen.findByText(/剪贴板不可用/)).toBeTruthy();
  });
});

describe("pasting a copied image back into a turn", () => {
  it("takes the path as the image it names, and sends no path", async () => {
    servesImages();
    const port = new MockPort();
    const attach = vi.spyOn(port, "attach").mockResolvedValue({
      path: ".reasonix/attachments/clipboard-new.png",
      ref: "@.reasonix/attachments/clipboard-new.png",
      image: true,
    });
    const onSubmit = vi.fn(async () => true);
    render(
      <Composer
        port={port as AgentPort}
        status={{ modelRef: "deepseek/m", workspaceRoot: "/workspace" } as SessionStatus}
        running={false}
        onSubmit={onSubmit}
        onChanged={vi.fn()}
        onError={vi.fn()}
      />,
    );
    const box = screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement;

    fireEvent.paste(box, { clipboardData: { files: [], getData: () => `@//docs/a.jpg/ 请看这张图` } });
    await waitFor(() => expect(attach).toHaveBeenCalled());
    // The box carries the words; the image is the chip beside them.
    expect(box.value).toBe("请看这张图");

    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    const sent = String((onSubmit.mock.calls[0] as unknown[])[0]);
    expect(sent).toContain("请看这张图");
    expect(sent).not.toContain("@//");
    expect(sent).not.toContain("docs/a.jpg");
  });

  it("takes a picture outside the workspace as the image it names", async () => {
    servesImages();
    const port = new MockPort();
    const attach = vi.spyOn(port, "attach").mockResolvedValue({
      path: ".reasonix/attachments/clipboard-new.png",
      ref: "@.reasonix/attachments/clipboard-new.png",
      image: true,
    });
    const onSubmit = vi.fn(async () => true);
    render(
      <Composer
        port={port as AgentPort}
        status={{ modelRef: "deepseek/m", workspaceRoot: "/workspace" } as SessionStatus}
        running={false}
        onSubmit={onSubmit}
        onChanged={vi.fn()}
        onError={vi.fn()}
      />,
    );
    const box = screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement;

    fireEvent.paste(box, { clipboardData: { files: [], getData: () => "@//c/:\\a.jpg/ 请看这张图" } });
    await waitFor(() => expect(attach).toHaveBeenCalled());
    expect(box.value).toBe("请看这张图");

    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    const sent = String((onSubmit.mock.calls[0] as unknown[])[0]);
    expect(sent).not.toContain("@//");
    expect(sent).not.toContain("a.jpg");
  });

  it("refuses a picture outside the workspace that no grant covers, and grants nothing", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => imageResponse(false, 403, "text/plain")));
    const port = new MockPort();
    const attach = vi.spyOn(port, "attach");
    const dropRefs = vi.spyOn(port, "dropRefs");
    const onError = vi.fn();
    render(
      <Composer
        port={port as AgentPort}
        status={{ modelRef: "deepseek/m", workspaceRoot: "/workspace" } as SessionStatus}
        running={false}
        onSubmit={vi.fn(async () => true)}
        onChanged={vi.fn()}
        onError={onError}
      />,
    );
    const box = screen.getByRole("combobox", { name: "任务输入" });

    fireEvent.paste(box, { clipboardData: { files: [], getData: () => "@//c/:\\a.jpg/ 请看" } });
    await waitFor(() => expect(onError).toHaveBeenCalled());
    expect(attach).not.toHaveBeenCalled();
    // Naming a path is not a grant: a refused picture is not referred to either.
    expect(dropRefs).not.toHaveBeenCalled();
  });
});

describe("an image referenced in place outside the workspace", () => {
  const extPath = "c:/folder/outside.png";
  const token = "__reasonix_external_folder/ab12cd34ef56/outside.png";
  const body = `@${token} 请看这张图`;
  const item = {
    t: "user",
    id: "ext",
    text: body,
    attachments: [{ path: extPath, ref: token }],
  } as Extract<Item, { t: "user" }>;

  it("copies the absolute path so a paste reads the image back, not the opaque token", async () => {
    servesImages();
    const write = installClipboard(async () => {});
    render(<UserCard item={item} port={new MockPort()} />);
    fireEvent.click(screen.getByRole("button", { name: "复制" }));
    await waitFor(() => expect(write).toHaveBeenCalledWith(`@//c/:/folder/outside.png 请看这张图`));
  });

  it("draws the image from its absolute path and keeps the token out of the bubble", () => {
    render(<UserCard item={item} port={new MockPort()} />);
    expect(screen.getByRole("img", { name: "outside.png" }).getAttribute("src")).toBe(`/fixture/${extPath}`);
    // The opaque external-folder token is implementation detail; the bubble shows
    // the words the person typed, as it does for a stored workspace attachment.
    expect(screen.queryByText(body)).toBeNull();
    expect(screen.getByText("请看这张图")).toBeTruthy();
  });
});
