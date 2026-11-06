import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, WorkspaceChange } from "../port/port";
import { stagedState } from "../state/gittree";
import { useCommitCard } from "./CommitCard";
import { usePushCard } from "./GitPushCard";
import { StudioIcon } from "./StudioIcon";
import { UnifiedDiff } from "./UnifiedDiff";

interface Props {
  port: AgentPort;
  changes: WorkspaceChange[];
  branch?: string;
  shown: boolean;
  remote: boolean;
  onOpen: (path: string) => void;
  onChanged?: () => void;
}

// A new or untracked file is all additions, so it starts folded with a line
// saying what is inside; a file that was edited shows its diff at once.
const startsOpen = (c: WorkspaceChange) => c.status !== "A" && c.status !== "??";

// Past this many files the folded note under each new or untracked file would
// double the list, so it becomes a tag in the file's own header.
const MANY_FILES = 8;

/** Every changed file of the workspace's own repository on one page, each with
 *  its diff under a header that stages it, opens it or throws the change away. */
export function WorkbenchChangesPage({ port, changes, branch, shown, remote, onOpen, onChanged }: Props) {
  const refresh = () => onChanged?.();
  const commit = useCommitCard(port, refresh);
  const push = usePushCard(port, undefined, refresh);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const settled = changes.length > 0 && changes.every((c) => c.staged && !c.unstaged);
  const added = changes.reduce((n, c) => n + (c.insertions ?? 0), 0);
  const removed = changes.reduce((n, c) => n + (c.deletions ?? 0), 0);
  const many = changes.length > MANY_FILES;
  const untracked = changes.filter((c) => c.status === "??").length;
  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setFailure("");
    try {
      await work();
      onChanged?.();
    } catch (e) {
      setFailure(reason(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="wb-changes">
      <div className="wb-changes-head">
        <StudioIcon name="branch" />
        {branch && <b title={branch}>{branch}</b>}
        <span title={`${t("common.n_files", { n: changes.length })}${many ? ` ${t("workbench.modified_untracked", { m: changes.length - untracked, u: untracked })}` : ""} +${added} −${removed}`}>{t("common.n_files", { n: changes.length })}{many && ` ${t("workbench.modified_untracked", { m: changes.length - untracked, u: untracked })}`} <i data-tone="add">+{added}</i> <i data-tone="del">−{removed}</i></span>
        <span className="wb-changes-fill" />
        <button type="button" className="wb-changes-btn" data-action="workbench.stage-all" disabled={busy || changes.length === 0}
          onClick={() => void run(() => port.stageChanges({ all: true, unstage: settled }))}>
          {t(settled ? "workbench.unstage_all" : "workbench.stage_all")}
        </button>
        {commit.button}
        {!remote && push.button}
      </div>
      {commit.card}
      {!remote && push.card}
      {failure && <p className="workbench-error" role="alert">{failure}</p>}
      <div className="wb-changes-body">
        {changes.length === 0 && <p className="workbench-empty">{t("workbench.no_changes")}</p>}
        {changes.map((c) => (
          <ChangedFile key={c.path} port={port} change={c} shown={shown} busy={busy} many={many} onOpen={onOpen}
            onStage={(unstage) => void run(() => port.stageChanges({ paths: [c.path], unstage }))}
            onDiscard={() => void run(() => port.discardChange({ path: c.path }))} />
        ))}
        {changes.length > 0 && <p className="wb-changes-hint">{t("workbench.changes_page_hint")}</p>}
      </div>
    </div>
  );
}

function ChangedFile({ port, change: c, shown, busy, many, onOpen, onStage, onDiscard }: {
  port: AgentPort; change: WorkspaceChange; shown: boolean; busy: boolean; many: boolean;
  onOpen: (path: string) => void; onStage: (unstage: boolean) => void; onDiscard: () => void;
}) {
  const [open, setOpen] = useState(() => startsOpen(c));
  const [diff, setDiff] = useState<{ text: string; truncated: boolean } | null>(null);
  const [failed, setFailed] = useState("");
  const [asking, setAsking] = useState(false);
  const staged = stagedState([c]);
  const discardable = !c.oldPath && /^[MDT]+$/.test(c.status || "M");
  useEffect(() => {
    if (!open || !shown) return;
    let live = true;
    port.changeDiff(c.path).then((d) => live && setDiff({ text: d.diff, truncated: d.truncated })).catch((e) => live && setFailed(reason(e)));
    return () => { live = false; };
  }, [port, c.path, c.insertions, c.deletions, c.status, open, shown]);
  return (
    <section className="wb-change" data-open={open ? "" : undefined}>
      <div className="wb-change-head">
        <button type="button" className="wb-change-fold" data-action="workbench.change-fold" data-target={c.path} aria-expanded={open}
          aria-label={c.path} onClick={() => setOpen(!open)}>
          <StudioIcon name={open ? "down" : "chevron"} />
        </button>
        <button type="button" role="checkbox" className="workbench-stage" data-action="workbench.stage" data-target={c.path}
          aria-checked={staged} aria-label={t("workbench.stage_path", { path: c.path })} disabled={busy}
          onClick={() => onStage(c.staged === true && !c.unstaged)} />
        <span className="wb-change-status" data-status={c.status}>{c.status || "M"}</span>
        <span className="wb-change-path" title={c.path}>{c.path}</span>
        <span className="wb-change-acts">
          <button type="button" data-action="workbench.change-open" data-target={c.path} onClick={() => onOpen(c.path)}>{t("workbench.open_file")}</button>
          {discardable && (
            <button type="button" data-action="workbench.discard" data-target={c.path} data-tone={asking ? "danger" : undefined} disabled={busy}
              aria-label={t("workbench.discard_the_change_to_path", { path: c.path })}
              onClick={() => (asking ? onDiscard() : setAsking(true))} onBlur={() => setAsking(false)}>
              {t(asking ? "workbench.confirm_discard" : "workbench.discard")}
            </button>
          )}
        </span>
        {many && !startsOpen(c) && <span className="wb-change-tag">{t(c.status === "??" ? "workbench.tag_untracked" : "workbench.tag_new")}</span>}
        <span className="wb-change-count">
          {c.insertions !== undefined && <i data-tone="add">+{c.insertions}</i>}
          {c.deletions !== undefined && <i data-tone="del">−{c.deletions}</i>}
        </span>
      </div>
      {!open && !startsOpen(c) && !many && (
        <button type="button" className="wb-change-peek" data-action="workbench.change-fold" data-target={c.path} onClick={() => setOpen(true)}>
          {t(c.status === "??" ? "workbench.untracked_open_to_see" : "workbench.new_file_open_to_see")}
        </button>
      )}
      {open && (
        failed ? <p className="workbench-error" role="alert">{failed}</p>
          : !diff ? <p className="wb-change-wait">{t("common.loading")}</p>
            : (
              <UnifiedDiff diff={diff.text} label={c.path} truncated={diff.truncated} onLine={() => onOpen(c.path)} target={c.path} />
            )
      )}
    </section>
  );
}
