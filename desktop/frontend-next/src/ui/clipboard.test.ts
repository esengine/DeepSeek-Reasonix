// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { CLIP_MANIFEST, clipText, copyAll, pasteContent, readClipText } from "./clipboard";

const image = ".reasonix/attachments/clipboard-20260101-000000.000000-000001.png";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Reflect.deleteProperty(navigator, "clipboard");
});

describe("the clipboard contract", () => {
  it("keeps plain text plain: a copy with no pictures carries no marker", () => {
    expect(clipText({ text: "看看这段文字", images: [] })).toBe("看看这段文字");
  });

  it("round-trips the words and the stored paths", () => {
    const encoded = clipText({ text: "看看这张图", images: [{ path: image }] });
    expect(encoded).toContain('"path":"' + image + '"');
    const read = readClipText(encoded);
    expect(read.text).toBe("看看这张图");
    expect(read.images).toEqual([{ path: image }]);
  });

  it("never turns text it cannot parse into a lost message", () => {
    const raw = "刚刚那段 <!--reasonix:clipboard not json-->";
    expect(readClipText(raw)).toEqual({ text: raw, images: [] });
    expect(readClipText("纯文本 @.reasonix/attachments/x.png")).toEqual({ text: "纯文本 @.reasonix/attachments/x.png", images: [] });
  });

  it("reads the manifest before the text, so a re-paste does not upload the pixels again", () => {
    const manifest = JSON.stringify({ v: 1, text: "看看", images: [{ path: image }] });
    const data = { getData: (type: string) => (type === CLIP_MANIFEST ? manifest : "看看 <!--reasonix:clipboard {\"v\":1,\"text\":\"看看\",\"images\":[]}-->") } as unknown as DataTransfer;
    expect(pasteContent(data)).toEqual({ text: "看看", images: [{ path: image }] });
  });

  it("falls back to text/plain when the manifest is absent", () => {
    const data = { getData: (type: string) => (type === "text/plain" ? "只有文字" : "") } as unknown as DataTransfer;
    expect(pasteContent(data)).toEqual({ text: "只有文字", images: [] });
  });
});

describe("copy all", () => {
  const stub = (write: (items: ClipboardItem[]) => Promise<void>) => {
    class Item {
      constructor(readonly data: Record<string, Blob>) {}
    }
    vi.stubGlobal("ClipboardItem", Item);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { write } });
    vi.stubGlobal("fetch", async () => new Response(new Blob(["png"], { type: "image/png" }), { headers: { "content-type": "image/png" } }));
  };

  it("writes the words, the manifest and one picture as pixels", async () => {
    let carried: unknown;
    stub(async (items) => {
      carried = items;
    });
    await copyAll({ text: "看看这张图", images: [{ path: image, url: "/workspace/image?path=" + image }] });
    const item = carried as { data: Record<string, Blob> }[];
    expect(item).toHaveLength(1);
    const data = item[0].data;
    expect(Object.keys(data).sort()).toEqual(["image/png", "text/plain", CLIP_MANIFEST].sort());
    expect(await data["text/plain"].text()).toContain("看看这张图");
    const manifest = JSON.parse(await data[CLIP_MANIFEST].text());
    expect(manifest.images).toEqual([{ path: image }]);
  });

  it("still copies the words and paths when the host refuses the rich write", async () => {
    const writeText = vi.fn(async (_text: string) => {});
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { write: async () => { throw new Error("denied"); }, writeText } });
    class Item {
      constructor(readonly data: Record<string, Blob>) {}
    }
    vi.stubGlobal("ClipboardItem", Item);
    vi.stubGlobal("fetch", async () => new Response(new Blob(["png"], { type: "image/png" }), { headers: { "content-type": "image/png" } }));
    await copyAll({ text: "看看这张图", images: [{ path: image, url: "/img" }] });
    expect(writeText).toHaveBeenCalledOnce();
    expect(readClipText(String(writeText.mock.calls[0][0]))).toEqual({ text: "看看这张图", images: [{ path: image }] });
  });
});
