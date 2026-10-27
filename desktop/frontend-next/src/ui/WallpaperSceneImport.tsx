import { useCallback, useEffect, useRef, useState } from "react";
import type { AgentPort, Appearance } from "../port/port";
import type { Wallpaper } from "../port/look";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

interface Props {
  port: AgentPort;
  look: Appearance;
  onLook: (look: Appearance) => void;
  setPaper: (patch: Partial<Wallpaper>) => void;
  onError: (message: string) => void;
}

/** Picks a Wallpaper Engine folder and shows what was imported. Kept out of
 *  Appearance so that panel can stay about the picture it already knows. */
export function WallpaperSceneImport({ port, look, onLook, setPaper, onError }: Props) {
  const folder = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const input = folder.current;
    if (!input) return;
    input.setAttribute("webkitdirectory", "");
    input.setAttribute("directory", "");
  }, []);

  const pick = useCallback(
    async (chosen: FileList | null) => {
      const files = Array.from(chosen ?? []);
      const project = files.find((f) => f.name.toLowerCase() === "project.json");
      const scene = files.find((f) => f.name.toLowerCase() === "scene.pkg");
      if (!project || !scene) {
        onError(t("文件夹里需要 project.json 和 scene.pkg"));
        return;
      }
      const preview = files.find((f) => /^preview\.(jpe?g|png|webp|avif)$/i.test(f.name));
      setBusy(true);
      onError("");
      try {
        onLook(await port.uploadScene({ project, scene, preview }));
      } catch (e) {
        onError(reason(e));
      } finally {
        setBusy(false);
      }
    },
    [port, onLook, onError],
  );

  const paper = look.wallpaper;
  return (
    <>
      <input
        ref={folder}
        data-action="wallpaper.scene"
        type="file"
        multiple
        hidden
        onChange={(e) => {
          void pick(e.target.files);
          e.target.value = "";
        }}
      />
      <button className="paperpick" data-action="wallpaper.scene" data-busy={busy ? "" : undefined} onClick={() => folder.current?.click()}>
        <span className="plus" aria-hidden="true">
          <svg viewBox="0 0 16 16">
            <path d="M8 3.7v8.6M3.7 8h8.6" />
          </svg>
        </span>
        {t("导入 Wallpaper Engine…")}
      </button>
      {paper?.kind === "scene" && (
        <>
          <div className="prow">
            <span className="tx">{t("Wallpaper Engine 场景")}</span>
            <span className="now">{paper.title || t("未命名场景")}</span>
            {(paper.audioTracks?.length || paper.audios?.length || paper.audio) && (
              <button className="now nowbtn" data-action="wallpaper.sound" onClick={() => setPaper({ muted: !paper.muted })}>
                {paper.muted ? t("打开声音") : t("关闭声音")}
              </button>
            )}
          </div>
          <p className="note">{t("场景以主图和声音播放；粒子和着色器效果暂未再现。")}</p>
        </>
      )}
    </>
  );
}
