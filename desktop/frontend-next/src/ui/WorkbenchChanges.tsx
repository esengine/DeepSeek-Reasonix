import { useMemo, useState, type ReactNode } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, StageRequest, WorkspaceChange } from "../port/port";
import { buildTree, stagedState, type ChangeNode } from "../state/gittree";
import { StudioIcon } from "./StudioIcon";
import { workspaceDrag } from "./filedrag";

interface Props {
  port: AgentPort;
  workspaceRoot?: string;
  changes: WorkspaceChange[];
  // The repository below the workspace this list belongs to; absent is the workspace's own.
  repo?: string;
  // The path whose diff is on screen, "" when none is.
  current: string;
  onOpen: (path: string) => void;
  // Asked for when staging or discarding changed the tree. Absent means this
  // surface cannot refresh the list, so it offers neither.
  onChanged?: () => void;
  commit: { button: ReactNode; card: ReactNode };
  // What the head carries beyond the commit button: the push of this repository.
  actions?: ReactNode;
  below?: ReactNode;
  // Opens the changes page from this section's title; absent where no page can show this repository.
  onOpenPage?: () => void;
}

// A discard goes back to the last commit, so only a file that commit holds and
// that is modified or deleted can be offered it.
const discardable = (c: WorkspaceChange) => !c.oldPath && /^[MDT]+$/.test(c.status || "M");

// The rows drawn at once. A workspace can carry a hundred thousand changes and
// every row is DOM the page holds for as long as the panel is mounted, open or not.
const ROW_PAGE = 500;

const withRepo = (repo: string | undefined, req: StageRequest): StageRequest => (repo ? { ...req, repo } : req);

// What differs as a folder tree, without opening anything, and which of it the next
// commit will record: the index is what a commit reads, so it is shown and changed
// here, a folder at a time or a file at a time. A change can also be thrown away.
export function WorkbenchChanges({ port, workspaceRoot, changes, repo, current, onOpen, onChanged, commit, actions, below, onOpenPage }: Props) {
  const [folded, setFolded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [shut, setShut] = useState<Set<string>>(new Set());
  const [asking, setAsking] = useState("");
  const [limit, setLimit] = useState(ROW_PAGE);
  const tree = useMemo(() => buildTree(changes), [changes]);
  const settled = changes.length > 0 && changes.every((c) => c.staged && !c.unstaged);

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
  const stage = (req: StageRequest) => run(() => port.stageChanges(withRepo(repo, req)));
  const discard = (path: string) =>
    run(async () => {
      await port.discardChange(repo ? { repo, path } : { path });
      setAsking("");
    });
  const fold = (path: string) => setShut((s) => { const next = new Set(s); if (!next.delete(path)) next.add(path); return next; });

  const drawn = { left: limit, files: 0 };
  const rows = (nodes: ChangeNode[], depth: number): ReactNode =>
    nodes.map((node) => {
      if (drawn.left <= 0) return null;
      drawn.left--;
      const inset = { paddingInlineStart: 6 + depth * 12 };
      if (node.kind === "folder") {
        const open = !shut.has(node.path);
        return (
          <div key={`d:${node.path}`} role="group" aria-label={node.path}>
            <div className="workbench-change workbench-change-folder" data-staged={stagedState(node.files) === true ? "" : undefined}>
              {onChanged && (
                <button type="button" role="checkbox" className="workbench-stage" data-action="workbench.stage-folder" data-target={node.path}
                  aria-checked={stagedState(node.files)} aria-label={t("workbench.stage_path", { path: node.path + "/" })} disabled={busy}
                  onClick={() => void stage({ paths: node.files.map((f) => f.path), unstage: stagedState(node.files) === true })}>
                  <StudioIcon name="check" aria-hidden="true" />
                </button>
              )}
              <button type="button" className="workbench-change-row workbench-folder-row" style={inset} data-action="workbench.change-folder" data-target={node.path}
                {...workspaceDrag(workspaceRoot, repo ? `${repo}/${node.path}` : node.path)}
                aria-expanded={open} onClick={() => fold(node.path)}>
                <StudioIcon name={open ? "down" : "chevron"} aria-hidden="true" />
                <span>{node.name}</span>
                <small>{node.files.length}</small>
                {node.insertions > 0 && <i data-io="up">+{node.insertions}</i>}
                {node.deletions > 0 && <i data-io="down">-{node.deletions}</i>}
              </button>
            </div>
            {open && rows(node.children, depth + 1)}
          </div>
        );
      }
      const item = node.change;
      drawn.files++;
      return (
        <div key={`c:${item.path}`}>
          <div className="workbench-change" data-staged={item.staged ? "" : undefined}>
            {onChanged && (
              <button type="button" role="checkbox" className="workbench-stage" data-action="workbench.stage" data-target={item.path}
                aria-checked={stagedState([item])} aria-label={t("workbench.stage_path", { path: item.path })} disabled={busy}
                onClick={() => void stage({ paths: [item.path], unstage: item.staged === true && !item.unstaged })}>
                <StudioIcon name="check" aria-hidden="true" />
              </button>
            )}
            <button className="workbench-change-row" style={inset} data-action="workbench.diff" data-target={item.path} title={item.path}
              {...workspaceDrag(workspaceRoot, repo ? `${repo}/${item.path}` : item.path)}
              aria-current={current === item.path ? "page" : undefined} onClick={() => onOpen(item.path)}>
              <em data-status={item.status}>{item.status || "M"}</em>
              <span>{node.name}</span>
              {/* Absent counts print nothing: a binary file did not change
                  by zero lines, git simply did not count it. */}
              {item.insertions !== undefined && <i data-io="up">+{item.insertions}</i>}
              {item.deletions !== undefined && <i data-io="down">-{item.deletions}</i>}
            </button>
            {onChanged && discardable(item) && (
              <button type="button" className="workbench-discard" data-action="workbench.discard" data-target={item.path}
                aria-label={t("workbench.discard_the_change_to_path", { path: item.path })} title={t("workbench.discard_this_files_change")} disabled={busy} onClick={() => setAsking(item.path)}>
                <StudioIcon name="rewind" aria-hidden="true" />
              </button>
            )}
          </div>
          {asking === item.path && (
            <div className="workbench-discard-ask" role="group" aria-label={t("workbench.confirm_discard")}>
              <p>{t("workbench.discard_the_change_to_path_2", { path: item.path })}</p>
              {item.staged && item.unstaged && <p>{t("workbench.both_its_staged_part_and")}</p>}
              <div>
                <button type="button" className="btn sm" data-danger="" data-action="workbench.discard-confirm" data-target={item.path} disabled={busy} onClick={() => void discard(item.path)}>{t("workbench.discard")}</button>
                <button type="button" className="btn sm" data-weak="" data-action="workbench.discard-cancel" disabled={busy} onClick={() => setAsking("")}>{t("common.cancel")}</button>
              </div>
            </div>
          )}
        </div>
      );
    });

  return (
    <div className="workbench-changes">
      <div className="workbench-explorer-head">
        <button type="button" className="workbench-section-fold" data-action="workbench.section-fold" data-target="changes" aria-expanded={!folded}
          aria-label={t(folded ? "workbench.expand_section" : "workbench.collapse_section", { name: t("common.changes") })} onClick={() => setFolded(!folded)}>
          <StudioIcon name={folded ? "chevron" : "down"} />
        </button>
        {onOpenPage ? (
          <button type="button" className="workbench-section-title" data-action="workbench.changes-page" onClick={onOpenPage}>{t("common.changes")}</button>
        ) : <span>{t("common.changes")}</span>}
        <small>{t("common.n_files", { n: changes.length })}</small>
        {onChanged && (
          <button type="button" className="btn sm workbench-stage-all" data-action="workbench.stage-all" disabled={busy}
            onClick={() => void stage({ all: true, unstage: settled })}>
            {t(settled ? "workbench.unstage_all" : "workbench.stage_all")}
          </button>
        )}
        {onChanged && commit.button}
        {onChanged && actions}
      </div>
      {commit.card}
      {below}
      {failure && <p className="refine-error workbench-stage-error" role="alert">{failure}</p>}
      {!folded && rows(tree, 0)}
      {!folded && drawn.left <= 0 && changes.length > drawn.files && (
        <div className="workbench-change-more">
          <span>{t("workbench.n_more_not_shown", { n: changes.length - drawn.files })}</span>
          <button type="button" className="btn sm" data-weak="" data-action="workbench.more-changes" onClick={() => setLimit((n) => n + ROW_PAGE)}>
            {t("workbench.show_n_more", { n: ROW_PAGE })}
          </button>
        </div>
      )}
    </div>
  );
}
