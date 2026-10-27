// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { t } from "../i18n";
import type { AgentPort, Appearance } from "../port/port";
import type { SceneFolder } from "../port/look";
import { WallpaperSceneImport } from "./WallpaperSceneImport";

afterEach(cleanup);

const sceneLook: Appearance = {
  wallpaper: {
    url: "/bg.jpg",
    kind: "scene",
    title: "花海",
    audio: "/scene.mp3",
    parallax: 0.02,
    muted: false,
    volume: 0.5,
    opacity: 0.5,
    dim: 0.55,
    focusX: 0.5,
    focusY: 0.5,
  },
};

function files() {
  return [
    new File(['{"title":"花海"}'], "project.json", { type: "application/json" }),
    new File([new Uint8Array([1, 2, 3])], "scene.pkg"),
    new File([new Uint8Array([4])], "preview.jpg", { type: "image/jpeg" }),
  ];
}

describe("importing a Wallpaper Engine folder", () => {
  it("sends project.json, scene.pkg and the preview together", async () => {
    const uploadScene = vi.fn(async (_folder: SceneFolder) => sceneLook);
    const onLook = vi.fn();
    const onError = vi.fn();
    const { container } = render(
      <WallpaperSceneImport
        port={{ uploadScene } as unknown as AgentPort}
        look={{}}
        onLook={onLook}
        setPaper={() => {}}
        onError={onError}
      />,
    );
    const input = container.querySelector('input[data-action="wallpaper.scene"]') as HTMLInputElement;
    const [project, scene, preview] = files();
    fireEvent.change(input, { target: { files: [project, scene, preview] } });

    await waitFor(() => expect(uploadScene).toHaveBeenCalledTimes(1));
    expect(uploadScene.mock.calls[0][0]).toMatchObject({ project, scene, preview });
    expect(onLook).toHaveBeenCalledWith(sceneLook);
    expect(onError).toHaveBeenCalledWith("");
  });

  it("says what is missing instead of uploading half a folder", async () => {
    const uploadScene = vi.fn();
    const onError = vi.fn();
    const { container } = render(
      <WallpaperSceneImport
        port={{ uploadScene } as unknown as AgentPort}
        look={{}}
        onLook={() => {}}
        setPaper={() => {}}
        onError={onError}
      />,
    );
    const input = container.querySelector('input[data-action="wallpaper.scene"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File([new Uint8Array([1])], "scene.pkg")] } });

    expect(uploadScene).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith(t("文件夹里需要 project.json 和 scene.pkg"));
  });

  it("mutes a scene's soundtrack", () => {
    const setPaper = vi.fn();
    render(
      <WallpaperSceneImport
        port={{ uploadScene: vi.fn() } as unknown as AgentPort}
        look={sceneLook}
        onLook={() => {}}
        setPaper={setPaper}
        onError={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: t("关闭声音") }));
    expect(setPaper).toHaveBeenCalledWith({ muted: true });
  });
});
