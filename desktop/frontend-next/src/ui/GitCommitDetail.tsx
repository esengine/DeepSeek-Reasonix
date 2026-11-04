import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { GitCommitFiles } from "../port/workspace";
import { DiffView } from "./cards/DiffView";

type Read = { k: "loading" } | { k: "ready"; read: GitCommitFiles } | { k: "failed"; why: string };
type Shown = { path: string; diff?: string; truncated?: boolean; why?: string };

// git prints a file header above the first hunk; the row above says which file it is.
const fromFirstHunk = (diff: string) => (diff.startsWith("@@") ? diff : diff.slice(diff.indexOf("\n@@") + 1) || diff);

// What one commit changed, read when its row is opened and never changed from here:
// its files with their counts, and one file's diff within that commit at a time.
export function GitCommitDetail({ port, id, repo }: { port: AgentPort; id: string; repo?: string }) {
  const [read, setRead] = useState<Read>({ k: "loading" });
  const [shown, setShown] = useState<Shown | null>(null);

  useEffect(() => {
    let live = true;
    setRead({ k: "loading" });
    setShown(null);
    port.commitFiles(id, repo).then(
      (r) => live && setRead({ k: "ready", read: r }),
      (e) => live && setRead({ k: "failed", why: reason(e) }),
    );
    return () => { live = false; };
  }, [port, id, repo]);

  const open = (path: string) => {
    if (shown?.path === path) {
      setShown(null);
      return;
    }
    setShown({ path });
    port.commitDiff(id, path, repo).then(
      (d) => setShown((s) => (s?.path === path ? { path, diff: d.diff, truncated: d.truncated } : s)),
      (e) => setShown((s) => (s?.path === path ? { path, why: reason(e) } : s)),
    );
  };

  if (read.k === "loading") return <p className="gh-note" role="status">{t("common.loading")}</p>;
  if (read.k === "failed") return <p className="gh-note refine-error" role="alert">{read.why}</p>;
  const { files, truncated } = read.read;
  return (
    <div className="gh-detail">
      {files.length === 0 && <p className="gh-note">{t("workbench.this_commit_changed_no_files")}</p>}
      <ul className="gh-files">
        {files.map((f) => (
          <li key={f.path}>
            <button type="button" className="gh-file" data-action="workbench.commit-file" data-target={f.path} aria-expanded={shown?.path === f.path} title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path} onClick={() => open(f.path)}>
              <em data-status={f.status}>{f.status}</em>
              <span>{f.path.slice(f.path.lastIndexOf("/") + 1)}</span>
              {f.insertions !== undefined && <i data-io="up">+{f.insertions}</i>}
              {f.deletions !== undefined && <i data-io="down">-{f.deletions}</i>}
            </button>
            {shown?.path === f.path && (
              <div className="gh-diff">
                {shown.why && <p className="refine-error" role="alert">{shown.why}</p>}
                {shown.diff === undefined && !shown.why && <p className="gh-note" role="status">{t("common.loading")}</p>}
                {shown.diff !== undefined && (shown.diff.trim() === "" ? <p className="gh-note">{t("workbench.there_is_no_text_change")}</p> : <DiffView diff={fromFirstHunk(shown.diff)} path={f.path} />)}
                {shown.truncated && <p className="gh-note">{t("workbench.too_long_to_show_in")}</p>}
              </div>
            )}
          </li>
        ))}
      </ul>
      {truncated && <p className="gh-more">{t("workbench.only_the_first_files_are")}</p>}
    </div>
  );
}
