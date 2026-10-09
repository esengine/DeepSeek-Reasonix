import { Fragment, useCallback, useEffect, useState, type KeyboardEvent } from "react";
import { bytes } from "../i18n/format";
import { t } from "../i18n";
import type { UpdateProgress, VersionHub, VersionNotes } from "../port/port";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import { LazyMarkdown } from "./LazyMarkdown";
import { deltaSkippedCopy, failureCopy, releasePage } from "./versionFailure";

// A shell that declared no install: a build run from source, not a failure.
const NO_INSTALL = "studio.no_install";
// Work is running and would end with the restart; the person decides.
const RESTART_BUSY = "update.restart_busy";
// The release has no notes published: nothing to retry, only the page to read.
const NOTES_ABSENT = "studio.notes_absent";

// The panel answers three questions in the order a user asks them: what am I
// running, is something wrong with it, and how do I get off it. Every action
// here is one that actually works — a button that cannot do what it says is
// worse than no button.

type Port = {
  versions(): Promise<VersionHub>;
  pinVersion(v: string): Promise<void>;
  goToVersion(v: string): Promise<void>;
  restartToVersion(v: string, force: boolean): Promise<void>;
  onUpdateProgress(cb: (p: UpdateProgress) => void): () => void;
  versionNotes?(version: string, retry?: boolean): Promise<VersionNotes>;
};

// A document read stays read: it is kept for as long as the panel is mounted,
// and the kernel keeps it on disk past that.
type NotesState = { phase: "loading" } | { phase: "ok"; markdown: string } | { phase: "err"; why: string; absent: boolean };

// Phases during which a move owns the install and nothing else may start.
const MOVING = new Set<UpdateProgress["phase"]>(["downloading", "verifying", "downloaded", "applying", "authorizing", "relaunching"]);

function when(iso: string): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return "";
  const days = Math.floor((Date.now() - at) / 86400000);
  if (days <= 0) return t("今天");
  if (days === 1) return t("昨天");
  if (days < 30) return t("{n} 天前", { n: days });
  return new Date(at).toLocaleDateString();
}

// The phase is the sentence. Verifying gets its own because it is the pause
// after the bar fills, which otherwise reads as a hang on a large artifact.
function say(p: UpdateProgress): string {
  switch (p.phase) {
    case "downloading":
      if (p.delta_skipped) {
        return p.total > 0 ? t("下载完整安装包 {got} / {all}", { got: bytes(p.received), all: bytes(p.total) }) : t("下载完整安装包 {got}", { got: bytes(p.received) });
      }
      return p.total > 0 ? t("下载中 {got} / {all}", { got: bytes(p.received), all: bytes(p.total) }) : t("下载中 {got}", { got: bytes(p.received) });
    case "verifying":
      return t("校验签名…");
    case "downloaded":
      return t("准备安装…");
    case "applying":
      return t("正在安装…");
    case "authorizing":
      return t("等待系统授权…");
    case "idle":
      // Only reachable in the gap between the click and the first read that
      // sees the move: the panel is already showing this row as going.
      return t("准备中…");
    case "relaunching":
      return t("正在重启到新版本…");
    case "ready":
      return t("已下载，待重启");
    case "error":
      return t("安装失败");
  }
}

export function Versions({ port }: { port: Port }) {
  const [hub, setHub] = useState<VersionHub | null>(null);
  const [busy, setBusy] = useState(false);
  const [starting, setStarting] = useState("");
  const [failed, setFailed] = useState("");
  const [unread, setUnread] = useState("");
  const [uninstalled, setUninstalled] = useState(false);
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [later, setLater] = useState("");
  const [running, setRunning] = useState(0);
  const [openNotes, setOpenNotes] = useState("");
  const [notes, setNotes] = useState<Record<string, NotesState>>({});

  // The kernel says why it cannot answer — a shell that never declared an
  // install, a server that does not carry this at all. Folding that back into
  // null spent the one sentence it gave us and left the panel reading as a
  // request still in flight, forever.
  const reload = useCallback(() => {
    port
      .versions()
      .then((h) => {
        setHub(h);
        setUnread("");
        setUninstalled(false);
      })
      .catch((e) => {
        setHub(null);
        const none = e instanceof HttpError && e.reason?.code === NO_INSTALL;
        setUninstalled(none);
        setUnread(none ? "" : reason(e));
      });
  }, [port]);

  useEffect(reload, [reload]);
  useEffect(() => port.onUpdateProgress(setProgress), [port]);

  // Starting ends once the kernel's own answer names the move; from then on
  // the row follows that answer rather than a local guess.
  useEffect(() => {
    if (starting && progress?.version === starting && progress.phase !== "idle") setStarting("");
  }, [starting, progress]);

  const pin = async (v: string) => {
    setBusy(true);
    setFailed("");
    try {
      await port.pinVersion(v);
      reload();
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy(false);
    }
  };

  // Answered once the download is under way. It stops at a verified release
  // waiting for the restart, which is the person's to allow.
  const goTo = async (v: string) => {
    setStarting(v);
    setLater("");
    setRunning(0);
    setProgress(null);
    try {
      await port.goToVersion(v);
    } catch (e) {
      setProgress({ version: v, phase: "error", received: 0, total: 0, err: reason(e) });
      setStarting("");
    }
  };

  const restart = async (v: string, force: boolean) => {
    setFailed("");
    try {
      await port.restartToVersion(v, force);
      setRunning(0);
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code === RESTART_BUSY) {
        setRunning(Number(e.reason.params?.n) || 1);
        return;
      }
      setFailed(reason(e));
    }
  };

  const loadNotes = (v: string, retry: boolean) => {
    if (!port.versionNotes) return;
    setNotes((n) => ({ ...n, [v]: { phase: "loading" } }));
    port
      .versionNotes(v, retry)
      .then((r) => setNotes((n) => ({ ...n, [v]: { phase: "ok", markdown: r.markdown } })))
      .catch((e) =>
        setNotes((n) => ({
          ...n,
          [v]: { phase: "err", why: reason(e), absent: e instanceof HttpError && e.reason?.code === NOTES_ABSENT },
        })),
      );
  };

  const toggleNotes = (v: string) => {
    if (openNotes === v) {
      setOpenNotes("");
      return;
    }
    setOpenNotes(v);
    const have = notes[v];
    if (!have || have.phase === "err") loadNotes(v, false);
  };

  const closeNotes = (e: KeyboardEvent<HTMLElement>, v: string) => {
    if (e.key !== "Escape" || openNotes !== v) return;
    e.stopPropagation();
    setOpenNotes("");
    document.querySelector<HTMLElement>(`[aria-controls="vnotes-${v}"]`)?.focus();
  };

  if (uninstalled) {
    return <p className="acct-note">{t("当前是从源码启动的开发版，没有可以查看或切换的版本。安装版 Studio 会在这里列出可用的更新。")}</p>;
  }

  if (unread) {
    return (
      <div className="find" data-lvl="warn" role="alert">
        <span className="t">{t("无法读取版本信息")}</span>
        <span className="why">
          {unread}
          <button className="lnk" data-action="versions.reload" onClick={reload}>
            {t("重试")}
          </button>
        </span>
      </div>
    );
  }

  if (hub === null) {
    return <p className="acct-note">{t("正在读取版本…")}</p>;
  }

  // A shell that answers null (or an older one that omits the field) must not
  // be able to take the window down with it.
  const list = hub.versions ?? [];
  const dev = !hub.current || hub.current === "dev";
  const moving = progress !== null && MOVING.has(progress.phase) ? progress.version : "";
  const ready = progress?.phase === "ready" ? progress.version : "";
  const locked = busy || starting !== "" || moving !== "";
  const readyOlder = ready !== "" && list.some((v) => v.version === ready && v.older);
  const failure = progress?.phase === "error" ? failureCopy(progress, hub.current) : null;
  return (
    <div className="vers">
      <div className="vnow">
        <span className="cur">{hub.current || "dev"}</span>
        <span className="lb">{t(dev ? "本地构建" : "当前版本")}</span>
        {hub.pinned && !hub.stalePin && <span className="pin">{t("已固定")}</span>}
      </div>

      {/* Severity language is the transcript's: a coloured left rule, no icons.
          Pinned is not a problem, so it is ok-coloured; a stale pin is. */}
      {hub.err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("无法连接版本目录")}</span>
          <span className="why">{t("{err}　—— 本地功能不受影响，请稍后重试。", { err: hub.err })}</span>
        </div>
      )}
      {failed && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("操作未完成")}</span>
          <span className="why">{failed}</span>
        </div>
      )}
      {hub.pinned && !hub.stalePin && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("已固定在 {v}，不再提示新版本", { v: hub.pinned })}</span>
          <span className="why">
            {t("回退时会固定在所选版本，免得新版本提示把你带回刚离开的版本。")}
            <button className="lnk" data-action="versions.pin" onClick={() => pin("")} disabled={locked}>
              {t("取消固定")}
            </button>
          </span>
        </div>
      )}
      {hub.stalePin && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("固定版本为 {pinned}，当前运行的是 {current}", { pinned: hub.pinned, current: hub.current })}</span>
          <span className="why">
            {t("这个固定没有作用，下次启动时会自动清除。")}
            <button className="lnk" data-action="versions.pin" onClick={() => pin("")} disabled={busy}>
              {t("现在清除")}
            </button>
          </span>
        </div>
      )}
      {!hub.err && hub.newer && !hub.pinned && !ready && !moving && !failure && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("有新版本 {v}", { v: hub.latest })}</span>
          <span className="why">{t("可在下方对应行安装。下载完成后会先问你，再重启。")}</span>
        </div>
      )}
      {ready && later !== ready && (
        <div className="find" data-lvl={running > 0 ? "warn" : "ok"} role="status">
          <span className="t">
            {running > 0 ? t("有 {n} 项任务正在运行", { n: running }) : t("{v} 已下载并通过签名校验", { v: ready })}
          </span>
          <span className="why">
            {running > 0
              ? t("现在重启会中断它们。可以等任务结束后再重启。")
              : t("重启 Studio 后生效。重启会关闭当前窗口，稍后重启也可以。")}
            {readyOlder && t("较新版本写入的会话在旧版本中暂时无法打开，升级回去后即可恢复。")}
          </span>
          <span className="acts">
            <button className="btn sm" data-primary="" data-action="versions.restart" onClick={() => restart(ready, running > 0)}>
              {running > 0 ? t("仍然重启") : t("立即重启")}
            </button>
            <button className="btn sm" data-action="versions.later" onClick={() => { setLater(ready); setRunning(0); }}>
              {t("稍后")}
            </button>
          </span>
        </div>
      )}
      {failure && progress && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{failure.title}</span>
          <span className="why">
            {failure.why}
            {failure.manual && (
              <a className="lnk" href={releasePage(progress.version)} target="_blank" rel="noreferrer noopener">
                {t("下载完整安装包")}
              </a>
            )}
          </span>
          {progress.err && <span className="gapd">{progress.err}</span>}
        </div>
      )}

      {/* Newest first: the list reads as history, and where you are in it is
          marked the way every other "this one" in the app is. */}
      <div className="vlist">
        {list.map((v, i) => (
          <Fragment key={v.version}>
          <div
            className="vrow"
            data-open={openNotes === v.version ? "" : undefined}
            data-on={v.current ? "" : undefined}
            data-side={v.current ? "now" : v.older ? "past" : "ahead"}
            style={{ animationDelay: `${Math.min(i, 8) * 34}ms` }}
            data-action={v.hasNotes ? "versions.notes-close" : undefined}
            onKeyDown={v.hasNotes ? (e) => closeNotes(e, v.version) : undefined}
          >
            <span className="nm">{v.version}</span>
            <span className="ds">{t(v.current ? "正在运行" : v.older ? "更早的版本" : "更新的版本")}</span>
            {/* A row the catalog does not carry has no date. Saying so beats an
                empty column: it is why this version has no download page. */}
            <span className="sc">{v.publishedAt ? when(v.publishedAt) : v.current ? t("未发布") : ""}</span>
            {moving === v.version && progress ? (
              <>
                <span
                  className="sa"
                  data-keep=""
                  title={progress.delta_skipped ? deltaSkippedCopy(progress.delta_skipped) : undefined}
                  aria-describedby={progress.delta_skipped ? `delta-skipped-${v.version}` : undefined}
                >
                  {say(progress)}
                </span>
                {progress.delta_skipped && (
                  <span id={`delta-skipped-${v.version}`} className="sr-only">
                    {deltaSkippedCopy(progress.delta_skipped)}
                  </span>
                )}
              </>
            ) : starting === v.version ? (
              <span className="sa" data-keep="">{t("准备中…")}</span>
            ) : ready === v.version ? (
              <button className="sa lnk" data-keep="" data-action="versions.restart" onClick={() => { setLater(""); void restart(v.version, false); }}>
                {t("重启以完成安装")}
              </button>
            ) : (
              !v.current && (
                <button className="sa lnk" data-action="versions.activate" onClick={() => goTo(v.version)} disabled={locked}>
                  {t(v.older ? "回退到这个版本" : "安装这个版本")}
                </button>
              )
            )}
            {v.current && !hub.pinned && (
              <button className="sa lnk" data-action="versions.pin" onClick={() => pin(v.version)} disabled={locked}>
                {t("固定在这里")}
              </button>
            )}
            {v.hasNotes && port.versionNotes && (
              <button
                className="vn lnk"
                data-action="versions.notes"
                aria-expanded={openNotes === v.version}
                aria-controls={`vnotes-${v.version}`}
                onClick={() => toggleNotes(v.version)}
              >
                {t(openNotes === v.version ? "收起更新内容" : "更新内容")}
              </button>
            )}
          </div>
          {v.hasNotes && port.versionNotes && openNotes === v.version && (
            <NotesPanel id={`vnotes-${v.version}`} version={v.version} state={notes[v.version]} retry={() => loadNotes(v.version, true)} onKeyDown={(e) => closeNotes(e, v.version)} />
          )}
          </Fragment>
        ))}
      </div>
    </div>
  );
}

function NotesPanel({ id, version, state, retry, onKeyDown }: { id: string; version: string; state: NotesState | undefined; retry: () => void; onKeyDown: (e: KeyboardEvent<HTMLElement>) => void }) {
  const loading = !state || state.phase === "loading";
  return (
    <section id={id} className="vnotes" data-action="versions.notes-close" onKeyDown={onKeyDown} aria-label={t("{v} 的更新内容", { v: version })} aria-busy={loading}>
      {loading && <p className="acct-note">{t("正在读取更新内容…")}</p>}
      {state?.phase === "ok" && <LazyMarkdown text={state.markdown} images={false} />}
      {state?.phase === "err" && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("更新内容读取失败")}</span>
          <span className="why">
            {state.why}
            {!state.absent && (
              <button className="lnk" data-action="versions.notes-retry" onClick={retry}>
                {t("重试")}
              </button>
            )}
            <a className="lnk" href={releasePage(version)} target="_blank" rel="noreferrer noopener">
              {t("在 GitHub 查看")}
            </a>
          </span>
        </div>
      )}
    </section>
  );
}
