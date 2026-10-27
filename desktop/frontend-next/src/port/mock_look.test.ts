// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockLook } from "./mock_look";

afterEach(() => vi.restoreAllMocks());

describe("the fixture's scene import", () => {
  it("reads the project title and keeps the scene shape", async () => {
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: vi.fn(() => "blob:scene") });
    const look = new MockLook();
    const got = await look.uploadScene({
      project: { size: 20, text: async () => '{"title":"花海"}' } as unknown as File,
      scene: { size: 10 } as unknown as File,
    });
    expect(got.wallpaper).toMatchObject({
      kind: "scene",
      title: "花海",
      parallax: 0.02,
      muted: true,
    });
  });

  it("refuses a folder without the two files", async () => {
    const look = new MockLook();
    await expect(
      look.uploadScene({ project: { size: 0 } as File, scene: { size: 1 } as File }),
    ).rejects.toMatchObject({ reason: { code: "wallpaper.scene_invalid" } });
  });
});
