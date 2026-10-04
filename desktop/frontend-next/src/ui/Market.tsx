import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AccountState, AgentPort, MarketDetail, MarketKind, MarketPackage, MarketPlan } from "../port/port";
import { Outcome } from "./AddPlugin";
import { Group } from "./Group";
import { PlanConfirm } from "./MarketConfirm";
import { MyPackages, PublishForm } from "./MarketPublish";
import { MarketVote, approvalLabel } from "./MarketVote";
import { arrowRadios, arrowTabs } from "./tablist";

const KINDS: [MarketKind | "", string][] = [["", "全部"], ["skill", "技能"], ["plugin", "插件"], ["mcp", "MCP 服务"], ["theme", "主题"]];
type Sort = "recommended" | "trending" | "installs" | "new";
const SORTS: [Sort, string][] = [["recommended", "推荐"], ["trending", "近期热门"], ["installs", "安装最多"], ["new", "最新"]];
const KIND_NAME: Record<string, string> = { skill: "技能", plugin: "插件", mcp: "MCP 服务", theme: "主题" };

interface Props {
  port: AgentPort;
  onInstalled: () => void;
  onViewInstalled?: (kind: string, name: string) => void;
  onSignIn?: () => void;
}

// The market is one more place a source comes from. It lists what reviewers let
// through and hands the approved version to the same plan-then-install every
// pasted address goes through; the kernel holds the pin, this only shows it.
export function Market({ port, onInstalled, onViewInstalled, onSignIn }: Props) {
  const [connection, setConnection] = useState({ port, generation: 0 });
  const currentConnection = useRef(connection);
  currentConnection.current = connection;
  const [kind, setKind] = useState<MarketKind | "">("");
  const [sort, setSort] = useState<Sort>("recommended");
  const [q, setQ] = useState("");
  const [pinned, setPinned] = useState(true);
  const [rows, setRows] = useState<MarketPackage[] | null>(null);
  const [more, setMore] = useState(false);
  const [error, setError] = useState("");
  const [filterUnsupported, setFilterUnsupported] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [open, setOpen] = useState("");
  const asked = useRef(0);
  const nextOffset = useRef(0);
  const panel = useRef<HTMLDivElement>(null);

  if (connection.port !== port) {
    setConnection({ port, generation: connection.generation + 1 });
    setRows(null);
    setMore(false);
    setError("");
    setFilterUnsupported(false);
    setLoadingMore(false);
  }

  const load = (offset: number) => {
    const n = ++asked.current;
    setError("");
    setFilterUnsupported(false);
    if (offset > 0) setLoadingMore(true);
    else setMore(false);
    port
      .marketList({ kind, q: q.trim(), sort, offset, pinned })
      .then((page) => {
        if (n !== asked.current || currentConnection.current !== connection) return;
        nextOffset.current = offset + page.packages.length;
        setRows((prev) => {
          if (offset === 0 || !prev) return page.packages;
          const seen = new Set(prev.map((p) => p.slug));
          return [...prev, ...page.packages.filter((p) => !seen.has(p.slug))];
        });
        setMore(page.packages.length >= page.limit);
        setLoadingMore(false);
      })
      .catch((e) => {
        if (n !== asked.current || currentConnection.current !== connection) return;
        setError(reason(e));
        setFilterUnsupported(pinned && e instanceof HttpError && e.reason?.code === "market.filter_unsupported");
        setLoadingMore(false);
        if (offset === 0) setRows([]);
      });
  };

  useEffect(() => {
    ++asked.current;
    setRows(null);
    setMore(false);
    nextOffset.current = 0;
    setError("");
    setLoadingMore(false);
    const timer = setTimeout(() => load(0), q ? 250 : 0);
    return () => { clearTimeout(timer); ++asked.current; };
  }, [port, kind, sort, q, pinned]); // eslint-disable-line react-hooks/exhaustive-deps

  if (open) {
    return (
      <Entry
        key={connection.generation}
        port={port}
        slug={open}
        onSignIn={onSignIn}
        onViewInstalled={onViewInstalled}
        onBack={() => setOpen("")}
        onInstalled={() => {
          if (currentConnection.current !== connection) return;
          onInstalled();
          load(0);
        }}
      />
    );
  }

  return (
    <div className="mkt" ref={panel} role="region" aria-label={t("社区市场")} tabIndex={-1}>
      <div className="mkt-bar">
        <input
          className="mkt-q"
          type="search"
          data-action="market.search"
          value={q}
          placeholder={t("搜索技能、插件、MCP 服务与主题")}
          aria-label={t("搜索社区市场")}
          onChange={(e) => setQ(e.target.value)}
        />
        <div className="seg" data-text role="radiogroup" aria-label={t("类型")} data-action-keydown="market.kind" onKeyDown={arrowRadios}>
          {KINDS.map(([id, name]) => (
            <button key={id || "all"} role="radio" aria-checked={kind === id} tabIndex={kind === id ? 0 : -1} data-action="market.kind" data-value={id || "all"} onClick={() => setKind(id)}>
              {t(name)}
            </button>
          ))}
        </div>
        <div className="seg" data-text role="radiogroup" aria-label={t("排序")} data-action-keydown="market.sort" onKeyDown={arrowRadios}>
          {SORTS.map(([id, name]) => (
            <button key={id} role="radio" aria-checked={sort === id} tabIndex={sort === id ? 0 : -1} data-action="market.sort" data-value={id} onClick={() => setSort(id)}>
              {t(name)}
            </button>
          ))}
        </div>
        <label className="mkt-pin">
          <input type="checkbox" data-action="market.pinned" checked={pinned} onChange={(e) => setPinned(e.target.checked)} />
          {t("只看已固定")}
        </label>
      </div>
      {error && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("无法读取社区市场")}</span>
          <span className="why">{error}</span>
          {(filterUnsupported || rows?.length === 0) && <div className="acts">
            {filterUnsupported && <button className="act" data-action="market.show-all" onClick={() => { panel.current?.focus(); setPinned(false); }}>{t("查看全部包")}</button>}
            {!filterUnsupported && <button className="act" data-action="market.retry" onClick={() => { panel.current?.focus(); setRows(null); load(0); }}>{t("重试")}</button>}
          </div>}
        </div>
      )}
      {rows === null && !error && <div className="empty" role="status">{t("正在读取…")}</div>}
      {rows?.length === 0 && !error && <div className="empty">{t(pinned ? "没有找到已固定内容的包。可关闭筛选查看全部包。" : "没有找到匹配的包。")}</div>}
      <ul className="mkt-list">
        {rows?.map((p) => (
          <li key={p.slug}>
            <button className="mkt-row" data-action="market.open" data-value={p.slug} onClick={() => setOpen(p.slug)}>
              <span className="mkt-line">
                <span className="nm">{p.name}</span>
                <span className="mkt-kind">{t(KIND_NAME[p.kind] ?? p.kind)}</span>
                {p.verified && <span className="mkt-badge" data-tone="ok">{t("管理员标记可信")}</span>}
                {p.installed && (
                  <span className="mkt-badge">
                    {p.installed.version === p.latestVersion ? t("已安装") : t("可更新")}
                  </span>
                )}
                <span className="mkt-n">{t("{n} 次安装", { n: p.installCount })}</span>
                <span className="mkt-n mkt-rate">{approvalLabel(p)}</span>
              </span>
              <span className="mkt-line">
                <span className="mkt-sum">{p.summary}</span>
                <span className="mkt-meta mkt-id">{`@${p.handle} · v${p.latestVersion}`}</span>
                {/* Outside the truncating meta so a narrow row loses the handle, never this. */}
                <span className="mkt-pinned" data-on={p.pinned ? "" : undefined} title={p.pinned === false ? t("审核时没有记录内容摘要，需要信任发布者后安装") : undefined}>
                  {t(p.pinned === undefined ? "固定状态未知" : p.pinned ? "已固定" : "未固定")}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      {more && (
        <button className="act" data-action="market.more" aria-live="polite" disabled={loadingMore} onClick={() => { panel.current?.focus({ preventScroll: true }); load(nextOffset.current); }}>
          {t(loadingMore ? "正在读取…" : error && !filterUnsupported ? "重试" : "加载更多")}
        </button>
      )}
    </div>
  );
}

function Entry({ port, slug, onBack, onInstalled, onViewInstalled, onSignIn }: { port: AgentPort; slug: string; onBack: () => void; onInstalled: () => void; onViewInstalled?: (kind: string, name: string) => void; onSignIn?: () => void }) {
  const [d, setD] = useState<MarketDetail | null>(null);
  const [plan, setPlan] = useState<MarketPlan | null>(null);
  const [result, setResult] = useState<{ plan: MarketPlan; stage: "done" | "retry" } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const panel = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let live = true;
    setD(null);
    setError("");
    port.marketDetail(slug)
      .then((detail) => { if (live) setD(detail); })
      .catch((e) => { if (live) setError(reason(e)); });
    return () => { live = false; };
  }, [port, slug, attempt]);

  const update = !!d?.installed && d.installed.version !== d.package.latestVersion;
  // trust is the person accepting a version no reviewer pinned; the kernel
  // then pins the install to this preview's digest instead.
  const look = async (trust = false) => {
    setBusy(true);
    setError("");
    try {
      setPlan(await port.planMarket({ slug, replace: update, ...(trust ? { trust } : {}) }));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };
  const install = async () => {
    if (!plan) return;
    setBusy(true);
    setError("");
    try {
      const pin = plan.unreviewed ? { trust: true, digest: plan.contentDigest } : {};
      const out = await port.installMarket({ slug, version: plan.version, planId: plan.planId, replace: update, ...pin });
      setResult({ plan: out, stage: "done" });
      if (out.applied) onInstalled();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const back = (
    <button className="act" data-action="market.back" onClick={onBack}>
      {t("返回列表")}
    </button>
  );

  const done = result?.stage === "done" ? result.plan : null;
  if (done) {
    const installed = done.applied ? done.actions?.filter((action) => action.status === "done" && action.name) ?? [] : [];
    const location = d?.package.kind === "plugin" || d?.package.kind === "theme"
      ? installed.find((action) => action.kind === "plugin") : installed[0];
    return (
      <div className="mkt addpkg" data-stage="done" ref={panel} role="region" aria-label={slug} tabIndex={-1}>
        <Outcome plan={done} />
        {installed.length > 0 && <ul className="mkt-installed">{installed.map((action, i) => <li key={`${action.kind}:${action.name}:${i}`}>{action.name}</li>)}</ul>}
        <div className="acts">
          {back}
          {!done.ok && <button className="act" data-action="market.detail-retry" onClick={() => { panel.current?.focus(); setResult(done.applied ? { plan: done, stage: "retry" } : null); setPlan(null); setD(null); setAttempt((n) => n + 1); }}>{t("重试")}</button>}
          {location && onViewInstalled && (
            <button className="act" data-action="market.view-installed" onClick={() => onViewInstalled(location.kind, location.name!)}>{t("查看已安装能力")}</button>
          )}
        </div>
      </div>
    );
  }

  if (plan) {
    return <PlanConfirm key={plan.planId} slug={slug} plan={plan} busy={busy} error={error} onCancel={() => setPlan(null)} onInstall={() => void install()} />;
  }

  if (!d) {
    return (
      <div className="mkt" ref={panel} role="region" aria-label={slug} tabIndex={-1}>
        {error ? (
          <div className="find" data-lvl="err" role="alert">
            <span className="t">{t("无法读取 {name}", { name: slug })}</span>
            <span className="why">{error}</span>
          </div>
        ) : (
          <div className="empty" role="status">{t("正在读取…")}</div>
        )}
        <div className="acts">
          {back}
          {error && <button className="act" data-action="market.detail-retry" onClick={() => { panel.current?.focus(); setAttempt((n) => n + 1); }}>{t("重试")}</button>}
        </div>
      </div>
    );
  }

  const p = d.package;
  const v = d.approved;
  const current = !!d.installed && !update && result?.stage !== "retry";
  // A copied skill is never overwritten in place, so an update has to start
  // from its removal rather than fail at the last step.
  const stuck = update && p.kind === "skill";
  return (
    <div className="mkt mkt-entry" ref={panel} role="region" aria-label={slug} tabIndex={-1}>
      <div className="mkt-hd">
        <span className="nm">{p.name}</span>
        <span className="mkt-kind">{t(KIND_NAME[p.kind] ?? p.kind)}</span>
        {p.verified && <span className="mkt-badge" data-tone="ok">{t("管理员标记可信")}</span>}
      </div>
      {p.summary && <p className="mkt-sum">{p.summary}</p>}
      {p.description && <p className="mkt-desc">{p.description}</p>}
      {!d.pinned && !current && (
        <div className="find" data-lvl="warn" data-unpinned="">
          <span className="t">{t("内容未经审核固定")}</span>
          <span className="why">{t("审核时没有记录内容摘要，无法确认来源现在提供的仍是审核过的内容。只在你信任发布者 @{handle} 时安装。", { handle: p.handle })}</span>
        </div>
      )}
      <dl className="mkt-facts">
        <dt>{t("发布者")}</dt>
        <dd>@{p.handle}</dd>
        <dt>{t("审核版本")}</dt>
        <dd>{v?.version || p.latestVersion}</dd>
        <dt>{t("来源")}</dt>
        <dd className="mono">{v?.source || "—"}</dd>
        <dt>{t("固定内容")}</dt>
        <dd className={d.pinned ? "mono" : undefined} data-missing={d.pinned ? undefined : ""}>{d.pinned ? v?.contentHash : t("未固定——审核时没有记录内容摘要")}</dd>
        {p.repoUrl && (
          <>
            <dt>{t("仓库")}</dt>
            <dd className="mono">{p.repoUrl}</dd>
          </>
        )}
        {p.tags.length > 0 && (
          <>
            <dt>{t("标签")}</dt>
            <dd>{p.tags.join(" · ")}</dd>
          </>
        )}
      </dl>
      <MarketVote port={port} pkg={p} onSignIn={onSignIn} />
      {result?.stage === "retry" && <Outcome plan={result.plan} />}
      <div className="acts">
        <span className="note">
          {current
            ? t("已安装 {version}", { version: d.installed!.version })
            : stuck
              ? t("技能不会被原地覆盖：先在「已安装」里移除旧版本，再回来安装")
              : t("先列出将安装的全部内容，确认后才会写入")}
        </span>
        {back}
        {!current && !stuck && d.pinned && (
          <button className="act" data-action="market.inspect" data-primary disabled={busy} onClick={() => void look()}>
            {t(busy ? "读取中…" : update ? "查看更新内容" : "查看将安装的内容")}
          </button>
        )}
        {!current && !stuck && !d.pinned && (
          <button className="act" data-action="market.trust" data-primary disabled={busy} onClick={() => void look(true)}>
            {t(busy ? "读取中…" : "信任并安装")}
          </button>
        )}
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}

type View = "browse" | "mine" | "publish";
const VIEWS: [View, string][] = [["browse", "浏览"], ["mine", "我的发布"], ["publish", "发布"]];

// Installed and discover are two views of one subject, so they are tabs of one
// page rather than two sections: what the market adds shows up on the other tab.
// Publishing spends the account session, so it is offered only while signed in.
export function MarketGroup({ port, onInstalled, onViewInstalled, account, onSignIn, onApplying }: Props & { account: AccountState | null; onSignIn: () => void; onApplying?: (applying: boolean) => void }) {
  const connection = useRef({ port });
  if (connection.current.port !== port) connection.current = { port };
  const owner = connection.current;
  const [view, setView] = useState<View>("browse");
  const [applying, setApplying] = useState(false);
  const applyingChanged = useCallback((busy: boolean) => { setApplying(busy); onApplying?.(busy); }, [onApplying]);
  const handle = account?.signedIn ? account.user?.handle : undefined;
  const [draft, setDraft] = useState<{ port: AgentPort; handle: string; pkg: MarketPackage } | null>(null);
  if (draft && (draft.port !== port || draft.handle !== handle)) setDraft(null);
  const at = handle ? view : "browse";
  return (
    <Group id="market" title={t("社区市场")}
      hint={t("社区发布、经过审核的技能、插件、MCP 服务与主题。固定了审核内容的版本按审核时的内容安装，未固定的需要你信任发布者；安装前都会列出将写入的全部内容，与粘贴地址安装走同一套确认。")}>
      {handle ? (
        <div className="seg mkt-views" data-text role="radiogroup" aria-label={t("社区市场")} data-action-keydown="market.view" onKeyDown={arrowRadios}>
          {VIEWS.map(([id, name]) => (
            <button key={id} role="radio" aria-checked={at === id} tabIndex={at === id ? 0 : -1} disabled={applying} data-action="market.view" data-value={id} onClick={() => { if (id === "publish" && at !== "publish") setDraft(null); setView(id); }}>
              {t(name)}
            </button>
          ))}
        </div>
      ) : (
        account !== null && (
          <div className="mkt-signin">
            <span>{t("登录后可以在这里发布技能、插件、MCP 服务和主题，并查看审核进度。")}</span>
            <button className="act" data-action="market.signin" onClick={onSignIn}>
              {t("去登录")}
            </button>
          </div>
        )
      )}
      {at === "browse" && <Market port={port} onInstalled={onInstalled} onViewInstalled={onViewInstalled} onSignIn={onSignIn} />}
      {at === "mine" && <MyPackages key={handle} port={port} onInstalled={() => {
        if (connection.current === owner) onInstalled();
      }} onViewInstalled={onViewInstalled} onApplying={applyingChanged} onPublish={(pkg) => {
        if (!handle) return;
        setDraft({ port, handle, pkg }); setView("publish");
      }} />}
      {at === "publish" && handle && <PublishForm port={port} handle={handle} initial={draft?.pkg} onMine={() => { setDraft(null); setView("mine"); }} />}
    </Group>
  );
}

export function ExtTabs({ at, onPick, disabled }: { at: "installed" | "market"; onPick: (at: "installed" | "market") => void; disabled?: boolean }) {
  const tabs: ["installed" | "market", string][] = [["installed", "已安装"], ["market", "发现"]];
  return (
    <div className="seg mkt-tabs" data-text role="tablist" aria-label={t("扩展")} onKeyDown={arrowTabs}>
      {tabs.map(([id, name]) => (
        <button key={id} role="tab" aria-selected={at === id} tabIndex={at === id ? 0 : -1} disabled={disabled} data-action="extensions.tab" data-value={id} onClick={() => onPick(id)}>
          {t(name)}
        </button>
      ))}
    </div>
  );
}
