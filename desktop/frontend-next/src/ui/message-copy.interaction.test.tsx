// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageCopy } from "./MessageCopy";
import { CLIP_MANIFEST, readClipText } from "./clipboard";

const image = ".reasonix/attachments/clipboard-20260101-000000.000000-000001.png";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Reflect.deleteProperty(navigator, "clipboard");
});

function clipboard() {
  class Item {
    constructor(readonly data: Record<string, Blob>) {}
  }
  vi.stubGlobal("ClipboardItem", Item);
  const written: { data: Record<string, Blob> }[] = [];
  const write = vi.fn(async (items: { data: Record<string, Blob> }[]) => { written.push(...items); });
  const writeText = vi.fn(async (_text: string) => {});
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { write, writeText } });
  vi.stubGlobal("fetch", async () => new Response(new Blob(["png"], { type: "image/png" }), { headers: { "content-type": "image/png" } }));
  return { write, writeText, written };
}

describe("copying a message", () => {
  it("takes the words, the pictures and the paths a paste can restore", async () => {
    const user = userEvent.setup();
    const clip = clipboard();
    render(<MessageCopy text="看看这张图" images={[image]} imageURL={(path) => "/workspace/image?path=" + path} />);

    await user.click(screen.getByRole("button", { name: "复制全部" }));
    await waitFor(() => expect(clip.write).toHaveBeenCalledOnce());
    const data = clip.written[0].data;
    expect(Object.keys(data).sort()).toEqual(["image/png", "text/plain", CLIP_MANIFEST].sort());
    const plain = await data["text/plain"].text();
    expect(readClipText(plain)).toEqual({ text: "看看这张图", images: [{ path: image }] });
    expect(plain.startsWith("看看这张图")).toBe(true);
    expect(JSON.parse(await data[CLIP_MANIFEST].text()).images).toEqual([{ path: image }]);
    await waitFor(() => expect(screen.getByRole("button", { name: "已复制" })).toBeTruthy());
  });

  it("offers text only as the second entry, and that copy names no picture", async () => {
    const user = userEvent.setup();
    const clip = clipboard();
    render(<MessageCopy text="看看这张图" images={[image]} imageURL={(path) => "/img/" + path} />);

    await user.click(screen.getByRole("button", { name: "更多复制方式" }));
    await user.click(screen.getByRole("menuitem", { name: "仅复制文字" }));
    await waitFor(() => expect(clip.writeText).toHaveBeenCalledExactlyOnceWith("看看这张图"));
    expect(clip.write).not.toHaveBeenCalled();
  });

  it("keeps one plain button for a message with no pictures", async () => {
    const clip = clipboard();
    render(<MessageCopy text="只有文字" images={[]} imageURL={() => ""} />);
    expect(screen.queryByRole("button", { name: "更多复制方式" })).toBeNull();
    screen.getByRole("button", { name: "复制" }).click();
    await waitFor(() => expect(clip.writeText).toHaveBeenCalledExactlyOnceWith("只有文字"));
  });
});
