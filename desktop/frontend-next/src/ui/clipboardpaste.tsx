import { useCallback, useState, type ClipboardEvent } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { chipId, type Chip } from "./composerChips";
import { nameOf } from "./chipfile";
import { pasteIsLong, countLines } from "./intake";
import { pasteContent } from "./clipboard";

/** The paste path, kept out of the composer: a message copied with its pictures
 *  comes back as words plus whatever this workspace still holds, and every
 *  other paste stays exactly what it was. */
export function useClipboardPaste({ port, onError, attach, insert, hold }: {
  port: AgentPort;
  onError: (e: unknown) => void;
  attach: (files: File[]) => void;
  insert: (text: string) => void;
  hold: (chips: Chip[]) => void;
}) {
  const [lost, setLost] = useState(0);
  // A restore is an upload without bytes: sending before it lands would let the
  // pictures arrive after the message they belong to.
  const [restoring, setRestoring] = useState(0);

  // The host names the stored attachments; a path this workspace no longer
  // holds is counted, never written into the line as a bare path.
  const restore = useCallback((paths: string[]) => {
    setLost(0);
    setRestoring((n) => n + 1);
    port
      .restoreAttachments(paths)
      .then((answers) => {
        const ready = answers.flatMap((r) =>
          r.ref
            ? [{
                k: "attachment" as const,
                id: chipId(),
                name: nameOf(r.path ?? r.ref),
                state: "ready" as const,
                a: { path: r.path ?? "", ref: r.ref, image: r.image },
              }]
            : [],
        );
        if (ready.length > 0) hold(ready as Chip[]);
        setLost(answers.length - ready.length);
      })
      .catch(onError)
      .finally(() => setRestoring((n) => n - 1));
  }, [port, onError, hold]);

  const onPaste = useCallback((e: ClipboardEvent<HTMLTextAreaElement>, busy: boolean) => {
    if (busy) {
      e.preventDefault();
      return;
    }
    const clip = pasteContent(e.clipboardData);
    if (clip.images.length > 0) {
      e.preventDefault();
      if (clip.text) insert(clip.text + " ");
      restore(clip.images.map((image) => image.path));
      return;
    }
    // Clipboard files are copied into host-owned attachments. Their native paths
    // can belong to a source application's restricted or short-lived cache.
    const files = [...e.clipboardData.files];
    if (files.length > 0) {
      e.preventDefault();
      attach(files.map((file) => new File([file], file.name, { type: file.type })));
      return;
    }
    const body = e.clipboardData.getData("text/plain");
    if (!pasteIsLong(body)) return;
    // Long enough to bury the composer. It still goes with the turn — it is just
    // held beside the box instead of becoming it.
    e.preventDefault();
    hold([{ k: "paste", id: chipId(), body, lines: countLines(body) }]);
  }, [attach, hold, insert, restore]);

  return { lost, restoring, clear: () => setLost(0), onPaste };
}

/** Pictures a paste named that this workspace no longer holds. */
export function PasteLost({ n }: { n: number }) {
  if (n === 0) return null;
  return <p className="shotwarn" role="status">{t("{n} 张图片不在当前工作区，粘贴时已忽略", { n })}</p>;
}
