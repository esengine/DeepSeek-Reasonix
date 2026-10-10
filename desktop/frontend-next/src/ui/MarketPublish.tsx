import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, MarketKind, MarketPackage, MarketPublished } from "../port/port";
import { OwnInstall } from "./MarketOwn";
import { arrowRadios } from "./tablist";

const KINDS: [MarketKind, string][] = [["skill", "技能"], ["plugin", "插件"], ["mcp", "MCP 服务"], ["theme", "主题"]];

// What a source has to look like for the market to install it once approved;
// the kernel refuses anything else before it leaves the machine.
const PINNED_TREE = "固定到提交的 GitHub 目录，形如 https://github.com/owner/repo/tree/（40 位提交号）/子目录";
const SOURCE_TIP: Record<MarketKind, string> = {
  skill: "SKILL.md 的 https 地址，或 GitHub 仓库里某个技能的目录。",
  plugin: PINNED_TREE,
  mcp: ".mcp.json 或仓库的 https 地址，或 npm 包名。",
  theme: PINNED_TREE,
};

const STATUS: Record<string, [string, string | undefined]> = {
  pending: ["审核中", undefined],
  active: ["已公开", "ok"],
  rejected: ["未通过", "err"],
  hidden: ["已隐藏", undefined],
  private: ["私有", undefined],
};

interface Draft {
  kind: MarketKind;
  name: string;
  source: string;
  summary: string;
  description: string;
  repoUrl: string;
  version: string;
  tags: string[];
  tagInput: string;
  private: boolean;
  origin?: string;
}

const EMPTY: Draft = { kind: "skill", name: "", source: "", summary: "", description: "", repoUrl: "", version: "", tags: [], tagInput: "", private: false };

interface PublishProps { port: AgentPort; handle: string; onMine: () => void; onApplying?: (applying: boolean) => void; initial?: MarketPackage }

export function PublishForm(props: PublishProps) {
  const [owner, setOwner] = useState({ port: props.port, handle: props.handle, generation: 0 });
  if (owner.port !== props.port || owner.handle !== props.handle) {
    setOwner({ port: props.port, handle: props.handle, generation: owner.generation + 1 });
  }
  return <PublishDraft key={owner.generation} {...props} />;
}

// The form only collects; which sources are publishable and what the registry
// accepts are the kernel's and the registry's answers, shown as they come back.
function PublishDraft({ port, handle, onMine, onApplying, initial }: PublishProps) {
  const [d, setD] = useState<Draft>(() => initial ? {
    ...EMPTY, kind: initial.kind, name: initial.name, summary: initial.summary,
    description: initial.description, repoUrl: initial.repoUrl, tags: [...initial.tags],
    private: initial.status === "private", origin: initial.slug,
  } : EMPTY);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState<MarketPublished | null>(null);
  const set = (k: keyof Draft) => (e: { target: { value: string } }) => setD({ ...d, [k]: e.target.value });

  useEffect(() => {
    onApplying?.(busy);
    return () => onApplying?.(false);
  }, [busy, onApplying]);

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      setDone(
        await port.publishMarket({
          kind: d.kind, name: d.name, source: d.source, summary: d.summary, description: d.description,
          repoUrl: d.repoUrl, version: d.version, tags: [...d.tags.filter(Boolean), ...d.tagInput.split(/[,，]/).map((x) => x.trim()).filter(Boolean)],
          visibility: d.private ? "private" : "public",
        }),
      );
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <div className="mkt mkt-pub" data-stage="done">
        <div className="find" data-lvl="ok" role="status">
          {done.package.status === "private" ? (
            <>
              <span className="t">{t("已保存 {slug} {version}，仅自己可见", { slug: done.package.slug, version: done.version })}</span>
              <span className="why">{t("它不会进入审核，也不会在社区市场出现；在「我的发布」里可以安装它，或随时提交审核。")}</span>
            </>
          ) : (
            <>
              <span className="t">{t("已提交 {slug} {version}", { slug: done.package.slug, version: done.version })}</span>
              <span className="why">{t("审核通过后会在社区市场公开；在「我的发布」里可以看到审核进度。")}</span>
            </>
          )}
        </div>
        <div className="acts">
          <button className="act" data-action="market.publish-again" onClick={() => { setD(EMPTY); setDone(null); }}>
            {t("再发布一个")}
          </button>
          <button className="act" data-action="market.view" data-value="mine" data-primary onClick={onMine}>
            {t("查看我的发布")}
          </button>
        </div>
      </div>
    );
  }

  const ready = d.name.trim() !== "" && d.source.trim() !== "" && !busy;
  return (
    <form className="mkt mkt-pub" aria-busy={busy} data-action-submit="market.publish" data-action-keydown="market.publish"
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.nativeEvent.isComposing && event.target instanceof HTMLInputElement && event.target.dataset.value !== "name" && event.target.dataset.value !== "source") event.preventDefault();
      }} onSubmit={(event) => { event.preventDefault(); if (ready) void submit(); }}>
      {d.origin && <p className="mkt-sum">{t("从 {slug} 复用发布资料；请填写本次发布的来源地址。", { slug: d.origin })}</p>}
      <p className="mkt-sum">{t(d.private
        ? "以 @{handle} 的名义保存，仅自己可见，不提交审核。只收来源地址，不上传文件。"
        : "以 @{handle} 的名义提交，审核通过后公开。只收来源地址，不上传文件。", { handle })}</p>
      <div className="seg" data-text role="radiogroup" aria-label={t("类型")} data-action-keydown="market.draft" data-value="kind" onKeyDown={arrowRadios}>
        {KINDS.map(([id, name]) => (
          <button key={id} type="button" role="radio" aria-checked={d.kind === id} tabIndex={d.kind === id ? 0 : -1} disabled={busy} data-action="market.draft" data-value="kind" onClick={() => setD({ ...d, kind: id })}>
            {t(name)}
          </button>
        ))}
      </div>
      {d.kind === "theme" && <p className="mkt-tip">{t("主题以插件包发布，包里只能有主题；带技能、钩子或 MCP 服务的包请按插件发布。")}</p>}
      <div className="mkt-fields">
        <label>
          <span>{t("名称")}</span>
          <input value={d.name} disabled={busy} data-action="market.draft" data-value="name" placeholder="my-package" spellCheck={false} onChange={set("name")} />
          <em className="mkt-tip">{t("小写字母、数字、点、下划线、连字符，最多 64 个字符。")}</em>
          {d.origin && <em className="mkt-tip">{t("要发布这个包的新版本，请保持名称不变；改名会发布为另一个包。")}</em>}
        </label>
        <label>
          <span>{t("版本")}</span>
          <input value={d.version} disabled={busy} data-action="market.draft" data-value="version" placeholder="0.1.0" spellCheck={false} onChange={set("version")} />
          <em className="mkt-tip">{t("留空时新包为 0.1.0；更新只自动递增纯数字三段版本的补丁号，其他版本请明确填写。")}</em>
        </label>
        <label className="full">
          <span>{t("来源地址")}</span>
          <input className="mono" value={d.source} disabled={busy} autoFocus={!!d.origin} data-action="market.draft" data-value="source" spellCheck={false} onChange={set("source")} />
          <em className="mkt-tip">{t(SOURCE_TIP[d.kind])}</em>
        </label>
        <label className="full">
          <span>{t("摘要")}</span>
          <input value={d.summary} disabled={busy} data-action="market.draft" data-value="summary" maxLength={200} onChange={set("summary")} />
        </label>
        <label className="full">
          <span>{t("描述")}</span>
          <textarea rows={4} value={d.description} disabled={busy} data-action="market.draft" data-value="description" maxLength={8000} onChange={set("description")} />
        </label>
        <label>
          <span>{t("仓库")}</span>
          <input className="mono" value={d.repoUrl} disabled={busy} data-action="market.draft" data-value="repoUrl" placeholder="https://github.com/…" spellCheck={false} onChange={set("repoUrl")} />
        </label>
        {d.tags.map((tag, i) => <label key={i}>
          <span>{t("已有标签 {n}", { n: i + 1 })}</span>
          <input value={tag} disabled={busy} data-action="market.draft" data-value="tags" onChange={(e) => setD({ ...d, tags: d.tags.map((value, n) => n === i ? e.target.value : value) })} />
        </label>)}
        <label>
          <span>{t(d.origin ? "新增标签" : "标签")}</span>
          <input value={d.tagInput} disabled={busy} data-action="market.draft" data-value="tags" placeholder={t("用逗号分隔，最多 8 个")} onChange={set("tagInput")} />
        </label>
      </div>
      <label className="mkt-seen">
        <input type="checkbox" data-action="market.draft" data-value="visibility" disabled={busy} checked={d.private} onChange={(e) => setD({ ...d, private: e.target.checked })} />
        {t("仅自己可见：不提交审核，社区市场里只有你的账号能看到并安装")}
      </label>
      {error && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("没有提交成功")}</span>
          <span className="why">{error}</span>
        </div>
      )}
      <div className="acts">
        <span className="note">
          {d.private
            ? t("保存后只有你能看到；要公开时在「我的发布」里提交审核。")
            : t("提交后进入审核队列；审核员会固定审核时的内容，之后只安装那一份。")}
        </span>
        <button type="submit" className="act" data-action="market.publish" data-primary disabled={!ready}>
          {t(busy ? "提交中…" : d.private ? "保存为私有" : "提交审核")}
        </button>
      </div>
    </form>
  );
}

type MineProps = { port: AgentPort; onInstalled: () => void; onViewInstalled?: (kind: string, name: string) => void; onApplying?: (applying: boolean) => void; onPublish?: (pkg: MarketPackage) => void };

export function MyPackages(props: MineProps) {
  const [connection, setConnection] = useState({ port: props.port, generation: 0 });
  const currentConnection = useRef(connection);
  currentConnection.current = connection;
  // Reset before child effects can preview the previous account's slug on the new port.
  if (connection.port !== props.port) setConnection({ port: props.port, generation: connection.generation + 1 });
  return <PackageList key={connection.generation} {...props} onInstalled={() => {
    if (currentConnection.current === connection) props.onInstalled();
  }} />;
}

// Every row is the account's own package, so each can be installed here in
// whatever state review has it; a private one can also be sent to review.
function PackageList({ port, onInstalled, onViewInstalled, onApplying, onPublish }: MineProps) {
  const [rows, setRows] = useState<MarketPackage[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<MarketPackage | null>(null);
  const [sending, setSending] = useState("");
  const [sendError, setSendError] = useState<[string, string] | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    setError("");
    setRows(null);
    port.myMarket().then((rows) => live && setRows(rows)).catch((e) => {
      if (!live) return;
      setError(reason(e));
      setRows([]);
    });
    return () => { live = false; };
  }, [port, attempt]);

  const submit = async (slug: string) => {
    setSending(slug);
    setSendError(null);
    try {
      const pkg = await port.submitMarket(slug);
      setRows((prev) => prev?.map((p) => (p.slug === slug ? { ...p, ...pkg, installed: p.installed } : p)) ?? null);
      setAttempt((n) => n + 1);
    } catch (e) {
      setSendError([slug, reason(e)]);
    } finally {
      setSending("");
    }
  };

  if (open) {
    return (
      <OwnInstall
        port={port}
        pkg={open}
        onBack={() => setOpen(null)}
        onViewInstalled={onViewInstalled}
        onApplying={onApplying}
        onInstalled={() => {
          onInstalled();
          setAttempt((n) => n + 1);
        }}
      />
    );
  }

  return (
    <div className="mkt" aria-busy={rows === null}>
      {error && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("无法读取我的发布")}</span>
          <span className="why">{error}</span>
          <div className="acts"><button className="act" data-action="market.mine-retry" onClick={() => setAttempt((n) => n + 1)}>{t("重试")}</button></div>
        </div>
      )}
      {rows === null && <div className="empty" role="status">{t("正在读取…")}</div>}
      {rows?.length === 0 && !error && <div className="empty">{t("还没有发布过。")}</div>}
      <ul className="mkt-list">
        {rows?.map((p) => {
          const [label, tone] = STATUS[p.status] ?? [p.status, undefined];
          const current = !!p.installed && p.installed.version === p.latestVersion;
          // A copied skill is never overwritten in place; its update starts from removal.
          const stuck = !!p.installed && !current && p.kind === "skill";
          return (
            <li key={p.slug} className="mkt-row" data-static="">
              <span className="mkt-hd">
                <span className="nm">{p.name}</span>
                <span className="mkt-kind">{t(KINDS.find(([k]) => k === p.kind)?.[1] ?? p.kind)}</span>
                <span className="mkt-badge" data-tone={tone}>{t(label)}</span>
                {p.installed && <span className="mkt-badge">{current ? t("已安装") : t("可更新")}</span>}
              </span>
              {p.summary && <span className="mkt-sum">{p.summary}</span>}
              <span className="mkt-meta">
                <span className="mkt-id">{`@${p.handle} · v${p.latestVersion}`}</span>
                {p.status === "active" && <> · {t("{n} 次安装", { n: p.installCount })}</>}
                {p.status === "private" && <> · {t("仅你可见，未提交审核")}</>}
              </span>
              <span className="acts">
                {onPublish && <button className="act" data-action="market.prepare-version" data-value={p.slug} disabled={sending === p.slug} onClick={() => onPublish(p)}>{t("发布新版本")}</button>}
                {stuck && <span className="note">{t("技能不会被原地覆盖：先在「已安装」里移除旧版本，再回来安装")}</span>}
                {p.status === "private" && (
                  <button className="act" data-action="market.submit" data-value={p.slug} disabled={!!sending} onClick={() => void submit(p.slug)}>
                    {t(sending === p.slug ? "提交中…" : "提交审核")}
                  </button>
                )}
                {!current && !stuck && (
                  <button className="act" data-action="market.own-inspect" data-value={p.slug} onClick={() => setOpen(p)}>
                    {t(p.installed ? "查看更新内容" : "安装")}
                  </button>
                )}
              </span>
              {sendError?.[0] === p.slug && <span className="why" role="alert">{sendError[1]}</span>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
