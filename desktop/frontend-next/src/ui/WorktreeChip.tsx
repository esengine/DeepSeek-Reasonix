import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { WorkspaceInfo, WorktreeSelection, WorktreeSources } from "../port/workspace";
import type { AgentPort, WorkspaceGit } from "../port/port";
import { useWorkspaceBranches } from "./useWorkspaceBranches";
import { StudioIcon } from "./StudioIcon";
import { WorkspacePath, WorkspaceBranchName, compactWorkspaceValue, workspaceName } from "./WorkspacePath";
import { listenAction } from "./listen";
import { beside, pinToViewport, useFollow, zoom } from "./place";
import "../styles/worktree.css";

interface Props {
  port: AgentPort;
  onChanged: () => void;
  onSwitched?: (git: WorkspaceGit) => void;
  onError: (error: unknown) => void;
  sources: WorktreeSources;
  canCreate: boolean;
  selection: WorktreeSelection | null;
  branch: string;
  busy: boolean;
  changeCount: number;
  onSelect: (selection: WorktreeSelection | null) => void;
  refresh: () => Promise<WorktreeSources>;
}

export function WorktreeChip({ sources, canCreate, selection, branch, busy, changeCount, onSelect, refresh, port, onChanged, onSwitched, onError }: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [sourceError, setSourceError] = useState(false);
  const [workspace, setWorkspace] = useState<WorkspaceInfo | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const title = useId();
  const returnFocus = useRef(false);
  const close = () => { returnFocus.current = true; setOpen(false); setQuery(""); };
  const { locals, moving, reading, loadBranches, switchTo } = useWorkspaceBranches(port, onChanged, (git) => { onSwitched?.(git); close(); }, onError, sources.sourceRoot);
  useEffect(() => {
    if (!open && moving === null && returnFocus.current) { returnFocus.current = false; trigger.current?.focus(); }
  }, [open, moving]);
  const isolated = workspace?.current === sources.sourceRoot && !!workspace.isolated;
  const chosen = sources.sources.find((source) => source.ref === selection?.ref);
  const label = chosen?.name ?? selection?.ref ?? branch;
  const scope = useRef({ port, root: sources.sourceRoot });
  useEffect(() => {
    let live = true;
    if (scope.current.port !== port || scope.current.root !== sources.sourceRoot) {
      scope.current = { port, root: sources.sourceRoot };
      returnFocus.current = false; setOpen(false); setQuery("");
    }
    setWorkspace(null);
    void port.workspaces().then((value) => { if (live) setWorkspace(value); }).catch(() => {});
    return () => { live = false; };
  }, [port, sources.sourceRoot]);
  const put = useCallback(() => {
    if (!trigger.current || !panel.current) return;
    const anchor = trigger.current.getBoundingClientRect();
    const height = panel.current.offsetHeight * zoom();
    pinToViewport(panel.current, anchor.left, beside(anchor, height, innerHeight, "above", 10), 12);
  }, []);
  useFollow(open, put, `${query}:${reading}:${sourceError}:${!!selection}:${locals.length}`);
  useEffect(() => {
    if (!open) return;
    search.current?.focus();
    const outside = (event: MouseEvent) => {
      if (!panel.current?.contains(event.target as Node) && !trigger.current?.contains(event.target as Node)) close();
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.stopPropagation(); close(); }
      if (event.key !== "Tab") return;
      const stops = Array.from(panel.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input, summary") ?? []);
      const edge = event.shiftKey ? stops[0] : stops.at(-1);
      if (document.activeElement === edge) { event.preventDefault(); (event.shiftKey ? stops.at(-1) : stops[0])?.focus(); }
    };
    const stopOutside = listenAction(window, "mousedown", { action: "workspace.configure", listener: outside as EventListener });
    const stopKey = listenAction(window, "keydown", { action: "workspace.configure", listener: key as EventListener }, true);
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(put);
    if (panel.current) observer?.observe(panel.current);
    return () => { stopOutside(); stopKey(); observer?.disconnect(); };
  }, [open, put]);
  useEffect(() => { if (busy) setOpen(false); }, [busy]);
  const reload = () => {
    setSourceError(false); loadBranches();
    if (canCreate) void refresh().catch(() => setSourceError(true));
  };
  const show = () => { if (open) { close(); return; } setOpen(true); setQuery(""); reload(); };
  const selectWorktree = () => {
    setQuery("");
    onSelect({ sourceRoot: sources.sourceRoot, ref: (sources.sources.find((source) => !source.remote && source.name === branch) ?? sources.sources.find((source) => source.current))?.ref ?? "HEAD" });
  };
  const choices = selection ? sources.sources.map((source) => ({ ...source, worktree: "" })) : locals.map((source) => ({ ...source, ref: source.name, remote: false }));
  const selected = (source: typeof choices[number]) => selection ? selection.ref === source.ref : !!source.current;
  const shown = choices.filter((source) => source.name.toLowerCase().includes(query.toLowerCase())).sort((a, b) => Number(selected(b)) - Number(selected(a)));
  const failed = selection ? sourceError : reading === "failed";
  const loading = !selection && (reading === "idle" || reading === "loading") && locals.length === 0;
  return <div className="studio-branch-pop">
    <button ref={trigger} type="button" className="mode plain studio-branch worktree-trigger" data-on={selection || isolated ? "" : undefined} data-pending={moving !== null ? "" : undefined} data-action="workspace.configure" aria-label={t("配置工作目录")} title={selection ? label : branch} aria-haspopup="dialog" aria-expanded={open} disabled={busy || moving !== null} onClick={show}>
      <StudioIcon name="branch"/><span className="lb">{moving ? t("正在切换到 {branch}…", { branch: moving }) : selection || isolated ? t("独立 worktree · {branch}", { branch: compactWorkspaceValue(label) }) : t("当前目录 · {branch}", { branch: compactWorkspaceValue(branch) })}</span>{changeCount > 0 && <small>{t("{n} 个变更", { n: changeCount })}</small>}<StudioIcon name="down"/>
    </button>
    {open && createPortal(<div ref={panel} className="workspace-palette" role="dialog" aria-labelledby={title}>
      <div className="workspace-palette-heading"><span id={title}>{t("配置工作目录")}</span><button data-action="workspace.configure" className="workspace-palette-close" aria-label={t("关闭工作目录配置")} onClick={close}><StudioIcon name="close"/></button></div>
      <div className="workspace-palette-identity"><StudioIcon name="folder"/><strong title={sources.sourceRoot}>{workspaceName(sources.sourceRoot)}</strong><span>{t(selection ? "新建 worktree" : isolated ? "独立 worktree" : "当前目录")}</span></div>
      {canCreate && <div className="worktree-modes"><button data-action="workspace.worktree-toggle" aria-pressed={!selection} disabled={busy || moving !== null} onClick={() => { onSelect(null); setQuery(""); }}><StudioIcon name="folder"/>{t("使用当前目录")}</button><button data-action="workspace.worktree-toggle" aria-pressed={!!selection} disabled={busy || moving !== null || !sources.available} onClick={selectWorktree}><StudioIcon name="branch"/>{t("新建 worktree")}</button></div>}
      {selection && <div className="workspace-palette-base"><span>{t("起始分支")}</span><strong title={label}>{compactWorkspaceValue(label)}</strong><small>{t("任务分支自动创建")}</small></div>}
      <div className="workspace-palette-search"><StudioIcon name="search"/><input data-action-change="workspace.source-search" data-action-keydown="workspace.source-navigate" ref={search} type="search" aria-label={t("搜索分支")} placeholder={t(selection ? "搜索起始分支…" : "搜索本地分支…")} value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => {
        if (event.key === "ArrowDown") { event.preventDefault(); panel.current?.querySelector<HTMLElement>('[role="option"]:not(:disabled)')?.focus(); }
      }}/></div>
      {failed ? <div className="workspace-palette-empty" role="alert"><p>{t("分支读取失败")}</p><button data-action="workspace.source-open" className="btn" onClick={reload}>{t("重新读取")}</button></div> : loading ? <div className="workspace-palette-empty" role="status">{t("正在读取分支…")}</div> : <div className="worktree-sources" role="listbox" aria-label={t(selection ? "起始分支" : "本地分支")} data-action="workspace.source-navigate" onKeyDown={(event) => {
        const items = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"));
        const at = items.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === "ArrowDown" ? at + 1 : event.key === "ArrowUp" ? at - 1 : at;
        if (next !== at) { event.preventDefault(); items[(next + items.length) % items.length]?.focus(); }
        if (event.key === "Home" || event.key === "End") { event.preventDefault(); (event.key === "Home" ? items[0] : items.at(-1))?.focus(); }
      }}>
        {[false, true].map((remote) => <div key={String(remote)}>{remote && shown.some((source) => source.remote) && <small className="workspace-palette-group">{t("远程分支")}</small>}{shown.filter((source) => source.remote === remote).map((source) => <button data-action={selection ? "workspace.source-select" : "git.branch"} key={source.ref} type="button" role="option" aria-label={source.name} aria-selected={selected(source)} disabled={!selection && (!!source.worktree || moving !== null)} title={source.worktree ? `${source.name}\n${source.worktree}` : source.name} onClick={() => {
          if (selection) onSelect({ ref: source.ref, sourceRoot: sources.sourceRoot }); else switchTo(source.name);
        }}><StudioIcon name="branch"/><span className="workspace-palette-branch"><WorkspaceBranchName value={source.name}/></span>{source.worktree ? <small>{t("其他 worktree 占用")}</small> : selected(source) && <small>{t(selection ? "起点" : "当前")}</small>}<StudioIcon name={selected(source) ? "check" : "chevron"}/></button>)}</div>)}
        {shown.length === 0 && <p className="workspace-palette-empty">{t("没有匹配的分支")}</p>}
      </div>}
      <p className="workspace-palette-note">{t(selection ? "从已提交版本创建，任务和附件将在就绪后发送" : "选择后立即切换，同目录会话也会受影响")}</p>
      <div className="workspace-palette-footer"><WorkspacePath path={sources.sourceRoot} onResize={put}/></div>
    </div>, document.body)}
  </div>;
}
