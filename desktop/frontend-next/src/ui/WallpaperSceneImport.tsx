import { StudioIcon } from "./StudioIcon";
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
        onError(t("look.the_folder_needs_project_json"));
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
          <StudioIcon name="upload" />
        </span>
        {t("look.import_wallpaper_engine")}
      </button>
      {paper?.kind === "scene" && (
        <>
          <div className="prow">
            <span className="tx">{t("look.wallpaper_engine_scene")}</span>
            <span className="now">{paper.title || t("look.untitled_scene")}</span>
            {(paper.audioTracks?.length || paper.audios?.length || paper.audio) && (
              <button className="now nowbtn" data-action="wallpaper.sound" onClick={() => setPaper({ muted: !paper.muted })}>
                {paper.muted ? t("look.unmute_sound") : t("look.mute_sound")}
              </button>
            )}
          </div>
          <p className="note">{t("look.the_scene_plays_its_backdrop")}</p>
        </>
      )}
    </>
  );
}
