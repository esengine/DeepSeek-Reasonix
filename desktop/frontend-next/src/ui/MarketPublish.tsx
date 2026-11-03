import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, MarketKind, MarketPackage, MarketPublished } from "../port/port";
import { OwnInstall } from "./MarketOwn";
import { arrowRadios } from "./tablist";

const KINDS: [MarketKind, string][] = [["skill", "common.skills2"], ["plugin", "common.plugins"], ["mcp", "common.mCPServers"], ["theme", "common.theme"]];

// What a source has to look like for the market to install it once approved;
// the kernel refuses anything else before it leaves the machine.
const PINNED_TREE = "marketPublish.aGitHubDirectoryPinnedTo";
const SOURCE_TIP: Record<MarketKind, string> = {
  skill: "marketPublish.anHttpsAddressOfA",
  plugin: PINNED_TREE,
  mcp: "marketPublish.anHttpsAddressOfA2",
  theme: PINNED_TREE,
};

const STATUS: Record<string, [string, string | undefined]> = {
  pending: ["common.inReview", undefined],
  active: ["marketPublish.public", "ok"],
  rejected: ["marketPublish.rejected", "err"],
  hidden: ["marketPublish.hidden", undefined],
  private: ["marketPublish.private", undefined],
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
              <span className="t">{t("marketPublish.savedVisibleOnlyToYou", { slug: done.package.slug, version: done.version })}</span>
              <span className="why">{t("marketPublish.itSkipsReviewAndNever")}</span>
            </>
          ) : (
            <>
              <span className="t">{t("marketPublish.submitted", { slug: done.package.slug, version: done.version })}</span>
              <span className="why">{t("marketPublish.itGoesPublicInThe")}</span>
            </>
          )}
        </div>
        <div className="acts">
          <button className="act" data-action="market.publish-again" onClick={() => { setD(EMPTY); setDone(null); }}>
            {t("marketPublish.publishAnother")}
          </button>
          <button className="act" data-action="market.view" data-value="mine" data-primary onClick={onMine}>
            {t("marketPublish.seeMyPackages")}
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
      {d.origin && <p className="mkt-sum">{t("marketPublish.reuseDetailsFromEnterThe", { slug: d.origin })}</p>}
      <p className="mkt-sum">{t(d.private
        ? "marketPublish.savedAsVisibleOnlyTo"
        : "marketPublish.submittedAsAndMadePublic", { handle })}</p>
      <div className="seg" data-text role="radiogroup" aria-label={t("common.type")} data-action-keydown="market.draft" data-value="kind" onKeyDown={arrowRadios}>
        {KINDS.map(([id, name]) => (
          <button key={id} type="button" role="radio" aria-checked={d.kind === id} tabIndex={d.kind === id ? 0 : -1} disabled={busy} data-action="market.draft" data-value="kind" onClick={() => setD({ ...d, kind: id })}>
            {t(name)}
          </button>
        ))}
      </div>
      {d.kind === "theme" && <p className="mkt-tip">{t("marketPublish.aThemeIsPublishedAs")}</p>}
      <div className="mkt-fields">
        <label>
          <span>{t("common.name")}</span>
          <input value={d.name} disabled={busy} data-action="market.draft" data-value="name" placeholder="my-package" spellCheck={false} onChange={set("name")} />
          <em className="mkt-tip">{t("marketPublish.lowercaseLettersDigitsDotsUnderscore")}</em>
          {d.origin && <em className="mkt-tip">{t("marketPublish.keepThisNameToPublish")}</em>}
        </label>
        <label>
          <span>{t("common.version")}</span>
          <input value={d.version} disabled={busy} data-action="market.draft" data-value="version" placeholder="0.1.0" spellCheck={false} onChange={set("version")} />
          <em className="mkt-tip">{t("marketPublish.blankStartsANewPackage")}</em>
        </label>
        <label className="full">
          <span>{t("marketPublish.sourceAddress")}</span>
          <input className="mono" value={d.source} disabled={busy} autoFocus={!!d.origin} data-action="market.draft" data-value="source" spellCheck={false} onChange={set("source")} />
          <em className="mkt-tip">{t(SOURCE_TIP[d.kind])}</em>
        </label>
        <label className="full">
          <span>{t("common.summary")}</span>
          <input value={d.summary} disabled={busy} data-action="market.draft" data-value="summary" maxLength={200} onChange={set("summary")} />
        </label>
        <label className="full">
          <span>{t("marketPublish.description")}</span>
          <textarea rows={4} value={d.description} disabled={busy} data-action="market.draft" data-value="description" maxLength={8000} onChange={set("description")} />
        </label>
        <label>
          <span>{t("common.repository")}</span>
          <input className="mono" value={d.repoUrl} disabled={busy} data-action="market.draft" data-value="repoUrl" placeholder="https://github.com/…" spellCheck={false} onChange={set("repoUrl")} />
        </label>
        {d.tags.map((tag, i) => <label key={i}>
          <span>{t("marketPublish.existingTag", { n: i + 1 })}</span>
          <input value={tag} disabled={busy} data-action="market.draft" data-value="tags" onChange={(e) => setD({ ...d, tags: d.tags.map((value, n) => n === i ? e.target.value : value) })} />
        </label>)}
        <label>
          <span>{t(d.origin ? "marketPublish.addTags" : "common.tags")}</span>
          <input value={d.tagInput} disabled={busy} data-action="market.draft" data-value="tags" placeholder={t("marketPublish.commaSeparatedUpTo8")} onChange={set("tagInput")} />
        </label>
      </div>
      <label className="mkt-seen">
        <input type="checkbox" data-action="market.draft" data-value="visibility" disabled={busy} checked={d.private} onChange={(e) => setD({ ...d, private: e.target.checked })} />
        {t("marketPublish.onlyMeSkipReviewIn")}
      </label>
      {error && (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("marketPublish.theSubmissionDidNotGo")}</span>
          <span className="why">{error}</span>
        </div>
      )}
      <div className="acts">
        <span className="note">
          {d.private
            ? t("marketPublish.onlyYouWillSeeIt")
            : t("marketPublish.itJoinsTheReviewQueue")}
        </span>
        <button type="submit" className="act" data-action="market.publish" data-primary disabled={!ready}>
          {t(busy ? "marketPublish.submitting" : d.private ? "marketPublish.saveAsPrivate" : "marketPublish.submitForReview")}
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
          <span className="t">{t("marketPublish.couldNotReadYourPackages")}</span>
          <span className="why">{error}</span>
          <div className="acts"><button className="act" data-action="market.mine-retry" onClick={() => setAttempt((n) => n + 1)}>{t("common.retry")}</button></div>
        </div>
      )}
      {rows === null && <div className="empty" role="status">{t("common.loading")}</div>}
      {rows?.length === 0 && !error && <div className="empty">{t("marketPublish.nothingPublishedYet")}</div>}
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
                {p.installed && <span className="mkt-badge">{current ? t("common.installed") : t("common.updateAvailable")}</span>}
              </span>
              {p.summary && <span className="mkt-sum">{p.summary}</span>}
              <span className="mkt-meta">
                <span className="mkt-id">{`@${p.handle} · v${p.latestVersion}`}</span>
                {p.status === "active" && <> · {t("common.installs", { n: p.installCount })}</>}
                {p.status === "private" && <> · {t("marketPublish.visibleOnlyToYouNot")}</>}
              </span>
              <span className="acts">
                {onPublish && <button className="act" data-action="market.prepare-version" data-value={p.slug} disabled={sending === p.slug} onClick={() => onPublish(p)}>{t("marketPublish.publishNewVersion")}</button>}
                {stuck && <span className="note">{t("common.aSkillIsNotOverwritten")}</span>}
                {p.status === "private" && (
                  <button className="act" data-action="market.submit" data-value={p.slug} disabled={!!sending} onClick={() => void submit(p.slug)}>
                    {t(sending === p.slug ? "marketPublish.submitting" : "marketPublish.submitForReview")}
                  </button>
                )}
                {!current && !stuck && (
                  <button className="act" data-action="market.own-inspect" data-value={p.slug} onClick={() => setOpen(p)}>
                    {t(p.installed ? "common.seeWhatTheUpdateInstalls" : "common.install")}
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
