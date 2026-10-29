import { useState, type MouseEvent } from "react";
import { t } from "../i18n";
import type { AgentPort, WorkspaceChange } from "../port/port";
import type { GitRepoRow } from "../port/workspace";
import { useCommitCard } from "./CommitCard";
import { usePushCard } from "./GitPushCard";
import { useRepoChanges } from "./useRepoChanges";
import { StudioIcon } from "./StudioIcon";
import { WorkbenchChanges } from "./WorkbenchChanges";
import { WorkbenchHistory } from "./WorkbenchHistory";

interface Props {
  port: AgentPort;
  workspaceRoot?: string;
  // The workspace's own repository's changes, kept current by the pane. A folder
  // of projects has none of its own: each repository below it reads its own.
  changes: WorkspaceChange[];
  repos: GitRepoRow[];
  // Why the repositories could not be listed, in words, and the way to ask again.
  failure?: string;
  onRetry?: () => void;
  // Moves when the agent has written, so a repository the pane does not watch is read again.
  wrote: number;
  // The workspace-relative path whose diff is on screen, "" when none is.
  current: string;
  onOpen: (path: string) => void;
  onMenu?: (e: MouseEvent<HTMLElement>, path: string, folder: boolean) => void;
  onChanged?: () => void;
  remote: boolean;
}

const join = (repo: string, path: string) => (repo ? `${repo}/${path}` : path);

// The Git panel: for each repository of the workspace, what changed as a tree to
// stage, discard and commit, the push of the branch, and its history. A workspace
// that is one repository shows just that; a folder of projects shows one section per
// repository, each acting on its own.
export function WorkbenchGit({ port, workspaceRoot, changes, repos, failure = "", onRetry, wrote, current, onOpen, onMenu, onChanged, remote }: Props) {
  // A list that could not be read says so, and the workspace is shown as the one
  // repository it was before there was a list: the changes the pane holds are real.
  if (failure) {
    return (
      <>
        <p className="refine-error workbench-stage-error" role="alert">
          {failure}
          {onRetry && <button type="button" className="btn sm" data-action="workbench.repos-reload" onClick={onRetry}>{t("重新读取")}</button>}
        </p>
        <RepoSection port={port} workspaceRoot={workspaceRoot} repo="" changes={changes} current={current} onOpen={onOpen} onMenu={onMenu} onChanged={onChanged} remote={remote} />
      </>
    );
  }
  if (repos.length === 0) return null;
  const several = repos.length > 1 || repos[0].dir !== "";
  if (!several) {
    return <RepoSection port={port} workspaceRoot={workspaceRoot} repo="" changes={changes} current={current} onOpen={onOpen} onMenu={onMenu} onChanged={onChanged} remote={remote} />;
  }
  return (
    <div className="workbench-git" data-repos={repos.length}>
      {repos.map((row) => (
        <Folder key={row.dir} port={port} workspaceRoot={workspaceRoot} row={row} wrote={wrote} current={current} onOpen={onOpen} onMenu={onMenu} remote={remote} />
      ))}
    </div>
  );
}

function Folder({ port, workspaceRoot, row, wrote, current, onOpen, onMenu, remote }: { port: AgentPort; workspaceRoot?: string; row: GitRepoRow; wrote: number; current: string; onOpen: (path: string) => void; onMenu?: Props["onMenu"]; remote: boolean }) {
  const [open, setOpen] = useState(true);
  const { changes, reload } = useRepoChanges(port, row.dir, open, wrote);
  return (
    <section className="workbench-repo" data-repo={row.dir}>
      <button type="button" className="workbench-repo-head" data-action="workbench.repo" data-target={row.dir} aria-expanded={open} onClick={() => setOpen(!open)}>
        <StudioIcon name={open ? "down" : "chevron"} aria-hidden="true" />
        <b>{row.name || row.dir}</b>
        <StudioIcon name="branch" aria-hidden="true" />
        <span>{row.branch}</span>
        <small>{changes.length}</small>
      </button>
      {open && <RepoSection port={port} workspaceRoot={workspaceRoot} repo={row.dir} changes={changes} current={current} onOpen={onOpen} onMenu={onMenu} onChanged={reload} remote={remote} />}
    </section>
  );
}

function RepoSection({ port, workspaceRoot, repo, changes, current, onOpen, onMenu, onChanged, remote }: {
  port: AgentPort; workspaceRoot?: string; repo: string; changes: WorkspaceChange[]; current: string; onOpen: (path: string) => void; onMenu?: Props["onMenu"]; onChanged?: () => void; remote: boolean;
}) {
  const refresh = () => onChanged?.();
  const commit = useCommitCard(port, refresh, repo || undefined);
  const push = usePushCard(port, repo || undefined, refresh);
  const inside = repo && current.startsWith(repo + "/") ? current.slice(repo.length + 1) : repo ? "" : current;
  return (
    <>
      <WorkbenchChanges
        port={port}
        workspaceRoot={workspaceRoot}
        repo={repo || undefined}
        changes={changes}
        current={inside}
        onOpen={(path) => onOpen(join(repo, path))}
        onMenu={onMenu}
        onChanged={onChanged}
        commit={commit}
        actions={remote ? null : push.button}
        below={remote ? null : push.card}
      />
      {!remote && <WorkbenchHistory port={port} repo={repo || undefined} />}
    </>
  );
}
