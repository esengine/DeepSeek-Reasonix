import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { DiffView } from "./cards/DiffView";

// git prints a file header above the first hunk — the "diff --git" line, the
// blob hashes, and both filenames. DiffView reads every line it is given as
// content, and the sheet already says which file this is, so the text starts at
// the first hunk. "@@" is the marker for one, which is why it is what gets
// looked for rather than the header lines being named one by one.
function fromFirstHunk(diff: string): string {
  if (diff.startsWith("@@")) return diff;
  const at = diff.indexOf("\n@@");
  return at < 0 ? diff : diff.slice(at + 1);
}

interface Props {
  port: AgentPort;
  path: string;
  onClose: () => void;
}

/** One listed change, read without leaving the window.
 *
 *  Portalled to the body rather than rendered where it is opened from: under a
 *  wallpaper the side column carries a backdrop-filter, and that makes it the
 *  containing block for anything fixed inside it — the overlay would be trapped
 *  in a 320px column. */
export function ChangePreview({ port, path, onClose }: Props) {
  const [diff, setDiff] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [failed, setFailed] = useState("");
  const veiled = useRef(false);

  useEffect(() => {
    let live = true;
    setDiff(null);
    setFailed("");
    port.changeDiff(path).then(
      (d) => {
        if (!live) return;
        setDiff(d.diff);
        setTruncated(d.truncated);
      },
      (e) => live && setFailed(reason(e)),
    );
    return () => {
      live = false;
    };
  }, [port, path]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);

  // Empty is an answer, not a missing one: a file git still lists can have been
  // put back to what it was between the list arriving and this being opened.
  const empty = diff !== null && diff.trim() === "";
  return createPortal(
    <div
      className="cpv"
      onMouseDown={(e) => { veiled.current = e.target === e.currentTarget; }}
      onMouseUp={(e) => { if (veiled.current && e.target === e.currentTarget) onClose(); }}
    >
      <div className="cpv-sheet" role="dialog" aria-modal="true" aria-label={path}>
        <div className="cpv-hd">
          <span className="p">{path}</span>
          <button className="btn sm" onClick={onClose}>
            {t("关闭")} <span className="esc">Esc</span>
          </button>
        </div>
        <div className="cpv-body">
          {failed && (
            <div className="find" data-lvl="err" role="alert">
              <span className="t">{failed}</span>
            </div>
          )}
          {!failed && diff === null && <span className="empty">{t("正在读这个文件的改动…")}</span>}
          {!failed && empty && <span className="empty">{t("这个文件现在和上一次提交一样")}</span>}
          {!failed && diff !== null && !empty && <DiffView diff={fromFirstHunk(diff)} path={path} />}
          {truncated && <p className="mnote">{t("改动太长，只显示了前面一段")}</p>}
        </div>
      </div>
    </div>,
    document.body,
  );
}
