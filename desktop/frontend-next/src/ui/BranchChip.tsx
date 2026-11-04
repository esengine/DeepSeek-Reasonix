import { useId, type CSSProperties } from "react";
import { t } from "../i18n";
import type { AgentPort, WorkspaceGit } from "../port/port";
import { Picker, type MenuItem } from "./Menu";
import { StudioIcon } from "./StudioIcon";
import { useWorkspaceBranches } from "./useWorkspaceBranches";

interface Props {
  port: AgentPort;
  // The work tree's git answer (/workspace/git), refreshed on the pane's tree
  // path. null is "not answered yet" and renders nothing; repo:false renders
  // a chip that says so — three states that must not read as one word.
  git: WorkspaceGit | null;
  changeCount: number;
  // The count is drawn elsewhere (the changed-files pill), so the chip keeps
  // only the branch name.
  countElsewhere?: boolean;
  // Called when a switch lands (status refresh) and when it moved the tree
  // (the change list, the branch reading and the workspace view re-read).
  onChanged: () => void;
  onSwitched?: (git: WorkspaceGit) => void;
  onError: (e: unknown) => void;
}

// Sizing belongs with the pane-aware positioning below; the stylesheet owns
// the card's visual treatment and hover/focus states.
const cardStyle: CSSProperties = {
  width: "max-content",
  minWidth: 0,
  maxWidth: "min(260px, calc(100vw - 24px))",
  whiteSpace: "normal",
  overflowWrap: "anywhere",
};

function fitCard(anchor: HTMLDivElement) {
  const card = anchor.querySelector<HTMLElement>(".studio-branch-card");
  if (!card) return;
  const box = anchor.getBoundingClientRect();
  const pane = anchor.closest(".pane")?.getBoundingClientRect();
  const scale = anchor.offsetWidth ? box.width / anchor.offsetWidth : 1;
  const left = Math.max(0, pane?.left ?? 0) + 12 * scale;
  const right = Math.min(innerWidth, pane?.right ?? innerWidth) - 12 * scale;
  card.style.maxWidth = `${Math.max(0, Math.min(260, (right - left) / scale))}px`;
  const width = card.getBoundingClientRect().width;
  const offset = (Math.max(left, Math.min(box.left, right - width)) - box.left) / scale;
  card.style.left = `${offset}px`;
  card.style.setProperty("--branch-tip-anchor", `${15 - offset}px`);
}

// The composer's branch chip: the workspace's branch as git itself names it,
// and the menu that moves the session to another one. The reading lives in
// the pane's tree refresh; this component owns only the choosing.
export function BranchChip({ port, git, changeCount, countElsewhere = false, onChanged, onSwitched, onError }: Props) {
  const branchTipId = useId();
  const { locals, moving, loadBranches, switchTo } = useWorkspaceBranches(port, onChanged, onSwitched, onError);
  const busy = moving !== null;
  // The list re-reads on open: a terminal or another window can move HEAD
  // while this pane is not looking, and a stale list would offer a switch
  // the repository no longer means.
  // A branch another worktree holds is listed but closed: git would refuse
  // the switch, so the row says where the branch lives instead of failing.
  const branchItems: MenuItem[] = [
    { value: "__branches", label: t("composer.local_branches"), right: String(locals.length), header: true },
    ...locals.map((b) => ({
      value: b.name,
      label: b.name,
      disabled: !!b.worktree,
      desc: b.worktree ? t("composer.checked_out_in_another_worktree") : undefined,
    })),
  ];

  if (git && !git.repo && git.absent) {
    const refused = git.absent === "refused";
    return (
      <div className="studio-branch-pop" onMouseEnter={(e) => fitCard(e.currentTarget)} onFocusCapture={(e) => fitCard(e.currentTarget)}>
        <div className="mode plain studio-branch" data-norepo="" data-absent={git.absent} role="img" aria-label={refused ? t("composer.git_refused_to_open_it") : t("composer.git_did_not_respond")} tabIndex={0} aria-describedby={branchTipId}>
          <span className="ic" aria-hidden="true"><StudioIcon name="branch" /></span>
          <span className="lb">{refused ? t("composer.git_refused_to_open_it") : t("composer.git_did_not_respond")}</span>
        </div>
        <div className="studio-branch-card" style={cardStyle} id={branchTipId} role="tooltip">
          <b>{refused ? t("composer.this_folder_has_a_git") : t("composer.this_folder_has_a_git_2")}</b>
          <span>{refused ? (git.detail ? t("composer.git_said_detail", { detail: git.detail }) : t("composer.run_git_status_in_a")) : t("composer.branches_cannot_be_shown_for")}</span>
        </div>
      </div>
    );
  }
  if (git && !git.repo) {
    return (
      <div className="studio-branch-pop" onMouseEnter={(e) => fitCard(e.currentTarget)} onFocusCapture={(e) => fitCard(e.currentTarget)}>
        <div className="mode plain studio-branch" data-norepo="" role="img" aria-label={t("composer.no_git_repository")} tabIndex={0} aria-describedby={branchTipId}>
          <span className="ic" aria-hidden="true"><StudioIcon name="branch" /></span>
          <span className="lb">{t("composer.no_git_repository")}</span>
        </div>
        <div className="studio-branch-card" style={cardStyle} id={branchTipId} role="tooltip">
          <b>{t("composer.this_workspace_is_not_version")}</b>
          <span>{t("composer.there_is_no_git_repository")}</span>
        </div>
      </div>
    );
  }
  if (!git?.repo) return null;
  return (
    <div className="studio-branch-pop" onMouseEnter={(e) => fitCard(e.currentTarget)} onFocusCapture={(e) => fitCard(e.currentTarget)}>
      <Picker
        className="mode plain studio-branch"
        data-action="git.branch"
        ariaLabel={t("composer.current_git_branch_branch", { branch: git.branch })}
        ariaDescribedBy={branchTipId}
        place="bottom"
        current={git.detached ? "" : git.branch}
        pending={busy}
        items={branchItems}
        onOpen={loadBranches}
        onPick={switchTo}
        label={<>
          <span className="ic" aria-hidden="true"><StudioIcon name="branch" /></span>
          <span className="lb">{moving === null ? git.branch : t("composer.switching_to_branch", { branch: moving })}</span>
          {changeCount > 0 && !countElsewhere && !busy && <small>{t("composer.n_changes", { n: changeCount })}</small>}
          <StudioIcon name="down" />
        </>}
      />
      <div className="studio-branch-card" style={cardStyle} id={branchTipId} role="tooltip">
        <b>{git.detached ? t("composer.detached_head_sha", { sha: git.branch }) : t("composer.current_branch_branch", { branch: git.branch })}</b>
        <span>{changeCount > 0 ? t("composer.current_workspace_n_local_changes", { n: changeCount }) : t("composer.current_workspace_future_tasks_continue")}</span>
        <small>{t("composer.refreshes_on_turns_and_writes")}</small>
      </div>
    </div>
  );
}
