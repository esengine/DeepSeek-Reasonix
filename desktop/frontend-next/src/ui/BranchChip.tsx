import { useCallback, useId, useState, type CSSProperties } from "react";
import { t } from "../i18n";
import type { AgentPort, WorkspaceBranch, WorkspaceGit } from "../port/port";
import { Picker, type MenuItem } from "./Menu";
import { StudioIcon } from "./StudioIcon";

interface Props {
  port: AgentPort;
  // The work tree's git answer (/workspace/git), refreshed on the pane's tree
  // path. null is "not answered yet" and renders nothing; repo:false renders
  // a chip that says so — three states that must not read as one word.
  git: WorkspaceGit | null;
  changeCount: number;
  // Called when a switch lands (status refresh) and when it moved the tree
  // (the change list, the branch reading and the workspace view re-read).
  onChanged: () => void;
  onSwitched?: () => void;
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
export function BranchChip({ port, git, changeCount, onChanged, onSwitched, onError }: Props) {
  const branchTipId = useId();
  const [busy, setBusy] = useState(false);
  const [locals, setLocals] = useState<WorkspaceBranch[]>([]);
  // The list re-reads on open: a terminal or another window can move HEAD
  // while this pane is not looking, and a stale list would offer a switch
  // the repository no longer means.
  const loadBranches = useCallback(() => {
    void port.branches().then((r) => setLocals(r.branches)).catch(() => setLocals([]));
  }, [port]);
  // A branch another worktree holds is listed but closed: git would refuse
  // the switch, so the row says where the branch lives instead of failing.
  const branchItems: MenuItem[] = [
    { value: "__branches", label: t("本地分支"), right: String(locals.length), header: true },
    ...locals.map((b) => ({
      value: b.name,
      label: b.name,
      disabled: !!b.worktree,
      desc: b.worktree ? t("已由其他工作树检出") : undefined,
    })),
  ];
  const switchTo = (name: string) => {
    if (busy) return;
    setBusy(true);
    void port.switchBranch(name)
      .then(() => { onChanged(); onSwitched?.(); })
      .catch(onError)
      .finally(() => setBusy(false));
  };

  if (git && !git.repo) {
    return (
      <div className="studio-branch-pop" onMouseEnter={(e) => fitCard(e.currentTarget)} onFocusCapture={(e) => fitCard(e.currentTarget)}>
        <div className="mode plain studio-branch" data-norepo="" role="img" aria-label={t("非 Git 仓库")} tabIndex={0} aria-describedby={branchTipId}>
          <span className="ic" aria-hidden="true"><StudioIcon name="branch" /></span>
          <span className="lb">{t("非 Git 仓库")}</span>
        </div>
        <div className="studio-branch-card" style={cardStyle} id={branchTipId} role="tooltip">
          <b>{t("此工作区未受版本控制")}</b>
          <span>{t("这里没有 Git 仓库，因此没有分支可显示或切换")}</span>
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
        ariaLabel={t("当前 Git 分支：{branch}", { branch: git.branch })}
        ariaDescribedBy={branchTipId}
        place="bottom"
        current={git.detached ? "" : git.branch}
        pending={busy}
        items={branchItems}
        onOpen={loadBranches}
        onPick={switchTo}
        label={<>
          <span className="ic" aria-hidden="true"><StudioIcon name="branch" /></span>
          <span className="lb">{git.branch}</span>
          {changeCount > 0 && <small>{t("{n} 个变更", { n: changeCount })}</small>}
          <StudioIcon name="down" />
        </>}
      />
      <div className="studio-branch-card" style={cardStyle} id={branchTipId} role="tooltip">
        <b>{git.detached ? t("HEAD 分离 · {sha}", { sha: git.branch }) : t("当前分支 · {branch}", { branch: git.branch })}</b>
        <span>{changeCount > 0 ? t("当前工作区 · {n} 个本地变更", { n: changeCount }) : t("当前工作区 · 后续任务继续使用此分支")}</span>
        <small>{t("随回合与写入刷新 · 非实时 · 点击可切换")}</small>
      </div>
    </div>
  );
}
