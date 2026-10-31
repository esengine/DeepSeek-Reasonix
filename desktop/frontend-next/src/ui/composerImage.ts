import { useCallback } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { nameOf } from "./chipfile";
import { chipId, type Chip } from "./composerChips";
import { isAbsolutePath, resolveImagePath } from "./imagepaths";

// How long an image gets to answer before it counts as unreadable. A paste is
// a small thing to ask for; a spinner is not.
const imageTimeout = 4000;

// Reads an image the host will serve, so it can ride into the turn as this
// turn's own attachment. A path the host will not serve answers with the
// reason rather than with nothing: the reader is told which picture could not
// be read, not left wondering why one image arrived and another did not.
export async function readImage(url: string): Promise<{ blob: Blob } | { why: string }> {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(imageTimeout) });
    if (!res.ok) return { why: t("图片无法读取（{code}）", { code: res.status }) };
    const blob = await res.blob();
    if (!blob.type.startsWith("image/")) return { why: t("图片格式无法识别：{type}", { type: blob.type }) };
    return { blob };
  } catch (e) {
    return { why: reason(e) };
  }
}

// A pasted image comes back as a path a copy wrote. It is taken as the image
// it names: the bytes are read from where it is and stored as this turn's
// attachment, so what the reader sees in the box is the image itself, and the
// path never becomes part of what is sent.
export function useTakeImages(port: AgentPort, workspaceRoot: string, attach: (files: File[]) => void,
  refer: (paths: string[], preview?: boolean) => void, onError: (e: unknown) => void) {
  return useCallback(
    async (paths: string[]) => {
      const files: File[] = [];
      const elsewhere: string[] = [];
      const refused: string[] = [];
      for (const path of paths) {
        // The host reads the picture where it is: inside the workspace, or
        // outside it where a grant covers the place it is in. A path no grant
        // covers is refused rather than referred to — naming a path is not a
        // grant, so a pasted path must not become read access.
        const read = await readImage(port.workspaceImageURL(path));
        if ("blob" in read) {
          files.push(new File([read.blob], nameOf(path), { type: read.blob.type }));
          continue;
        }
        if (isAbsolutePath(path)) refused.push(path);
        else elsewhere.push(resolveImagePath(path, workspaceRoot));
      }
      if (files.length > 0) attach(files);
      if (elsewhere.length > 0) refer(elsewhere, true);
      if (refused.length > 0) {
        onError(new Error(t("工作区外的图片无法读取：{path}", { path: refused.join("、") })));
      }
    },
    [attach, onError, port, refer, workspaceRoot],
  );
}

// A dropped file is referenced where it lives. Copying it in is what let a
// turn edit the copy and report the edit as done while the file the user
// pointed at never changed. Only the host can mint the token: whether a path
// is inside the workspace compares two spellings of one location.
//
// A pasted image comes back the other way round — as a path a copy wrote —
// and the reader wants it as the image it names: preview says so, and the
// path stays on the chip as the place it came from.
export function useRefIn(port: AgentPort, onError: (e: unknown) => void,
  addChips: (chips: Chip[]) => void) {
  return useCallback(
    (paths: string[], preview = false) => {
      port
        .dropRefs(paths)
        .then((refs) => {
          const took = refs.flatMap((r) =>
            r.ref
              ? [{
                  k: "attachment" as const,
                  id: chipId(),
                  name: nameOf(r.path ?? r.ref),
                  state: "ready" as const,
                  a: { path: r.path ?? "", ref: r.ref, image: r.image },
                  url: preview && r.image && r.path && !isAbsolutePath(r.path)
                    ? port.workspaceImageURL(r.path)
                    : undefined,
                }]
              : [],
          );
          if (took.length > 0) addChips(took);
          const refused = refs.flatMap((r) => (r.error ? [r.error] : []));
          if (refused.length > 0) onError(new Error(refused.join("\n")));
        })
        .catch(onError);
    },
    [addChips, onError, port],
  );
}
