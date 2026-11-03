import { Fragment, useCallback, useEffect, useState, type KeyboardEvent, type MouseEvent } from "react";
import { bytes } from "../i18n/format";
import { current, t } from "../i18n";
import type { UpdateProgress, VersionHub, VersionNotes } from "../port/port";
import { reason } from "../i18n/kernel";
import { refreshUpdateWatch } from "../state/updatewatch";
import { HttpError } from "../port/http_error";
import { LazyMarkdown } from "./LazyMarkdown";
import { NOTES_LANGUAGE_NAMES, splitNotes } from "./notesLanguages";
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
  versions(refresh?: boolean): Promise<VersionHub>;
  pinVersion(v: string): Promise<void>;
  goToVersion(v: string): Promise<void>;
  cancelVersion(v: string): Promise<void>;
  restartToVersion(v: string, force: boolean): Promise<void>;
  onUpdateProgress(cb: (p: UpdateProgress) => void): () => void;
  versionNotes?(version: string, retry?: boolean): Promise<VersionNotes>;
  openExternal?(url: string): Promise<void>;
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
  if (days <= 0) return t("versions.today");
  if (days === 1) return t("versions.yesterday");
  if (days < 30) return t("common.daysAgo", { n: days });
  return new Date(at).toLocaleDateString();
}

// The phase is the sentence. Verifying gets its own because it is the pause
// after the bar fills, which otherwise reads as a hang on a large artifact.
function say(p: UpdateProgress): string {
  switch (p.phase) {
    case "downloading":
      if (p.stalls) return t("versions.downloadPausedTheConnectionDelivered", { n: p.stalls });
      if (p.delta_skipped) {
        return p.total > 0 ? t("versions.downloadingTheFullPackage", { got: bytes(p.received), all: bytes(p.total) }) : t("versions.downloadingTheFullPackage2", { got: bytes(p.received) });
      }
      return p.total > 0 ? t("versions.downloading", { got: bytes(p.received), all: bytes(p.total) }) : t("versions.downloading2", { got: bytes(p.received) });
    case "verifying":
      return t("versions.verifyingTheSignature");
    case "downloaded":
      return t("versions.preparingToInstall");
    case "applying":
      return t("versions.installing");
    case "authorizing":
      return t("versions.waitingForPermission");
    case "idle":
      // Only reachable in the gap between the click and the first read that
      // sees the move: the panel is already showing this row as going.
      return t("versions.starting");
    case "relaunching":
      return t("versions.restartingIntoTheNewVersion");
    case "ready":
      return t("versions.downloadedWaitingForARestart");
    case "error":
      return t("versions.installFailed");
    case "cancelled":
      return t("versions.downloadCancelled");
  }
}

// The newest few older releases stay on the page; the rest are history one click away.
const PAST_SHOWN = 3;

// An installer is a file to save, which the window's own browser does not do.
function openInstallerPage(e: MouseEvent<HTMLAnchorElement>, port: Port, version: string) {
  if (!port.openExternal) return;
  e.preventDefault();
  void port.openExternal(releasePage(version)).catch(() => {});
}

export function Versions({ port }: { port: Port }) {
  const [hub, setHub] = useState<VersionHub | null>(null);
  const [busy, setBusy] = useState(false);
  const [starting, setStarting] = useState("");
  const [cancelling, setCancelling] = useState("");
  const [failed, setFailed] = useState("");
  const [unread, setUnread] = useState("");
  const [uninstalled, setUninstalled] = useState(false);
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [later, setLater] = useState("");
  const [running, setRunning] = useState(0);
  const [allPast, setAllPast] = useState(false);
  const [openNotes, setOpenNotes] = useState("");
  const [notes, setNotes] = useState<Record<string, NotesState>>({});

  // The kernel says why it cannot answer — a shell that never declared an
  // install, a server that does not carry this at all. Folding that back into
  // null spent the one sentence it gave us and left the panel reading as a
  // request still in flight, forever.
  const load = useCallback((refresh: boolean) => {
    port
      .versions(refresh)
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
  const reload = useCallback(() => load(true), [load]);

  useEffect(() => load(false), [load]);
  // Opening the page is a look at the releases, so the background watch reads now
  // instead of at its next daily turn: with automatic download chosen, a release
  // published since the last read starts downloading without a click.
  useEffect(() => { void refreshUpdateWatch(); }, []);
  useEffect(() => port.onUpdateProgress(setProgress), [port]);
  // The kernel's next frame ends the local "取消中…" state, whatever the
  // cancellation turned out to be.
  useEffect(() => {
    if (cancelling && progress?.version === cancelling && progress.phase !== "downloading" && progress.phase !== "verifying") {
      setCancelling("");
    }
  }, [cancelling, progress]);

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

  // The kernel stops the transfer and discards what it wrote. The row keeps
  // saying "取消中…" until its progress leaves the download, so a second click
  // cannot race the first.
  const cancel = async (v: string) => {
    if (cancelling) return;
    setCancelling(v);
    setFailed("");
    try {
      await port.cancelVersion(v);
    } catch (e) {
      setCancelling("");
      setFailed(reason(e));
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
    return <p className="acct-note">{t("versions.thisIsADevelopmentBuild")}</p>;
  }

  if (unread) {
    return (
      <div className="find" data-lvl="warn" role="alert">
        <span className="t">{t("versions.versionInformationCouldNotBe")}</span>
        <span className="why">
          {unread}
          <button className="lnk" data-action="versions.reload" onClick={reload}>
            {t("common.retry")}
          </button>
        </span>
      </div>
    );
  }

  if (hub === null) {
    return <p className="acct-note">{t("versions.loadingVersions")}</p>;
  }

  // A shell that answers null (or an older one that omits the field) must not
  // be able to take the window down with it.
  const list = hub.versions ?? [];
  const dev = !hub.current || hub.current === "dev";
  const moving = progress !== null && MOVING.has(progress.phase) ? progress.version : "";
  const ready = progress?.phase === "ready" ? progress.version : "";
  const locked = busy || starting !== "" || moving !== "";
  const readyOlder = ready !== "" && list.some((v) => v.version === ready && v.older);
  const past = list.filter((v) => v.older && !v.current);
  const held = new Set([moving, ready, hub.pinned, openNotes].filter(Boolean));
  const shown = allPast ? list : list.filter((v) => !v.older || v.current || past.indexOf(v) < PAST_SHOWN || held.has(v.version));
  const hidden = list.length - shown.length;
  const failure = progress?.phase === "error" ? failureCopy(progress, hub.current) : null;
  return (
    <div className="vers">
      <div className="vnow">
        <span className="cur">{hub.current || "dev"}</span>
        <span className="lb">{t(dev ? "versions.localBuild" : "versions.currentVersion")}</span>
        {hub.pinned && !hub.stalePin && <span className="pin">{t("common.pinned")}</span>}
      </div>

      {/* Severity language is the transcript's: a coloured left rule, no icons.
          Pinned is not a problem, so it is ok-coloured; a stale pin is. */}
      {hub.withdrawn && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("versions.wasWithdrawn", { v: hub.withdrawn.version })}</span>
          <span className="why">
            {hub.withdrawn.reason && t("versions.reason", { reason: hub.withdrawn.reason })}
            {hub.newer ? t("versions.updateToFromItsRow", { v: hub.latest }) : t("versions.pickAVersionBelow")}
          </span>
        </div>
      )}
      {hub.err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("versions.cannotReachTheVersionIndex")}</span>
          <span className="why">{t("versions.nothingLocalIsAffectedTry", { err: hub.err })}</span>
        </div>
      )}
      {failed && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("common.thatDidNotGoThrough")}</span>
          <span className="why">{failed}</span>
        </div>
      )}
      {hub.pinned && !hub.stalePin && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("versions.pinnedToNewerVersionsAre", { v: hub.pinned })}</span>
          <span className="why">
            {t("versions.aRollbackPinsTheVersion")}
            <button className="lnk" data-action="versions.pin" onClick={() => pin("")} disabled={locked}>
              {t("versions.unpin")}
            </button>
          </span>
        </div>
      )}
      {hub.stalePin && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("versions.pinnedToWhatIsRunning", { pinned: hub.pinned, current: hub.current })}</span>
          <span className="why">
            {t("versions.thisPinHoldsNothingAnd")}
            <button className="lnk" data-action="versions.pin" onClick={() => pin("")} disabled={busy}>
              {t("versions.clearItNow")}
            </button>
          </span>
        </div>
      )}
      {!hub.err && hub.newer && !hub.pinned && !hub.withdrawn && !ready && !moving && !failure && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("versions.isOut", { v: hub.latest })}</span>
          <span className="why">{t("versions.installItFromTheRow")}</span>
        </div>
      )}
      {ready && later !== ready && (
        <div className="find" data-lvl={running > 0 ? "warn" : "ok"} role="status">
          <span className="t">
            {running > 0 ? t("versions.tasksAreRunning", { n: running }) : t("versions.isDownloadedAndItsSignature", { v: ready })}
          </span>
          <span className="why">
            {running > 0
              ? t("versions.restartingNowWouldStopThem")
              : t("versions.itTakesEffectWhenStudio")}
            {readyOlder && t("versions.aSessionWrittenByA")}
          </span>
          <span className="acts">
            <button className="btn sm" data-primary="" data-action="versions.restart" onClick={() => restart(ready, running > 0)}>
              {running > 0 ? t("versions.restartAnyway") : t("versions.restartNow")}
            </button>
            <button className="btn sm" data-action="versions.later" onClick={() => { setLater(ready); setRunning(0); }}>
              {t("common.later")}
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
              <a className="lnk" href={releasePage(progress.version)} target="_blank" rel="noreferrer noopener" data-action="versions.full-installer" onClick={(e) => openInstallerPage(e, port, progress.version)}>
                {t("versions.downloadTheFullInstaller")}
              </a>
            )}
          </span>
          {progress.err && <span className="gapd">{progress.err}</span>}
        </div>
      )}

      {/* Newest first: the list reads as history, and where you are in it is
          marked the way every other "this one" in the app is. */}
      <div className="vlist">
        {shown.map((v, i) => (
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
            <span className="ds">{t(v.current ? "versions.runningNow" : v.older ? "versions.anEarlierVersion" : "versions.aNewerVersion")}</span>
            {/* A row the catalog does not carry has no date. Saying so beats an
                empty column: it is why this version has no download page. */}
            <span className="sc">{v.publishedAt ? when(v.publishedAt) : v.current ? t("versions.notPublished") : ""}</span>
            {moving === v.version && progress ? (
              <>
                <span className="sa" data-keep="">
                  <span
                    className="sa-txt"
                    title={progress.delta_skipped ? deltaSkippedCopy(progress.delta_skipped) : undefined}
                    aria-describedby={progress.delta_skipped ? `delta-skipped-${v.version}` : undefined}
                  >
                    {say(progress)}
                  </span>
                  {(progress.phase === "downloading" || progress.phase === "verifying") && (
                    <button
                      className="lnk"
                      data-action="versions.cancel"
                      onClick={() => void cancel(v.version)}
                      disabled={cancelling !== ""}
                    >
                      {cancelling === v.version ? t("common.stopping") : t("versions.cancelDownload")}
                    </button>
                  )}
                </span>
                {progress.delta_skipped && (
                  <span id={`delta-skipped-${v.version}`} className="sr-only">
                    {deltaSkippedCopy(progress.delta_skipped)}
                  </span>
                )}
              </>
            ) : starting === v.version ? (
              <span className="sa" data-keep="">{t("versions.starting")}</span>
            ) : ready === v.version ? (
              <button className="sa lnk" data-keep="" data-action="versions.restart" onClick={() => { setLater(""); void restart(v.version, false); }}>
                {t("versions.restartToFinish")}
              </button>
            ) : (
              !v.current && (
                <button className="sa lnk" data-action="versions.activate" onClick={() => goTo(v.version)} disabled={locked}>
                  {t(v.older ? "versions.rollBackToThisVersion" : "versions.installThisVersion")}
                </button>
              )
            )}
            {v.current && !hub.pinned && (
              <button className="sa lnk" data-action="versions.pin" onClick={() => pin(v.version)} disabled={locked}>
                {t("versions.pinHere")}
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
                {t(openNotes === v.version ? "versions.hideWhatSNew" : "versions.whatSNew")}
              </button>
            )}
          </div>
          {v.hasNotes && port.versionNotes && openNotes === v.version && (
            <NotesPanel id={`vnotes-${v.version}`} version={v.version} state={notes[v.version]} retry={() => loadNotes(v.version, true)} onKeyDown={(e) => closeNotes(e, v.version)} />
          )}
          </Fragment>
        ))}
        {hidden > 0 && (
          <button className="lnk vmore" data-action="versions.more" onClick={() => setAllPast(true)}>
            {t("versions.showEarlierVersions", { n: hidden })}
          </button>
        )}
      </div>
    </div>
  );
}

// The notes in the language the window draws in, with a switch to the other one
// when the document carries both.
function NotesBody({ markdown }: { markdown: string }) {
  const { first, english } = splitNotes(markdown);
  const [shown, setShown] = useState<"first" | "english">(english !== null && current() === "en" ? "english" : "first");
  if (english === null) return <LazyMarkdown text={first} images={false} />;
  return (
    <>
      <div className="vnotes-lang seg" role="group" aria-label={t("versions.languageOfTheNotes")}>
        <button data-action="versions.notes-lang" data-value="first" aria-pressed={shown === "first"} onClick={() => setShown("first")}>{NOTES_LANGUAGE_NAMES.first}</button>
        <button data-action="versions.notes-lang" data-value="english" aria-pressed={shown === "english"} onClick={() => setShown("english")}>{NOTES_LANGUAGE_NAMES.english}</button>
      </div>
      <LazyMarkdown text={shown === "english" ? english : first} images={false} />
    </>
  );
}

function NotesPanel({ id, version, state, retry, onKeyDown }: { id: string; version: string; state: NotesState | undefined; retry: () => void; onKeyDown: (e: KeyboardEvent<HTMLElement>) => void }) {
  const loading = !state || state.phase === "loading";
  return (
    <section id={id} className="vnotes" data-action="versions.notes-close" onKeyDown={onKeyDown} aria-label={t("versions.whatSNewIn", { v: version })} aria-busy={loading}>
      {loading && <p className="acct-note">{t("common.loadingWhatSNew")}</p>}
      {state?.phase === "ok" && <NotesBody markdown={state.markdown} />}
      {state?.phase === "err" && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("versions.whatSNewCouldNot")}</span>
          <span className="why">
            {state.why}
            {!state.absent && (
              <button className="lnk" data-action="versions.notes-retry" onClick={retry}>
                {t("common.retry")}
              </button>
            )}
            <a className="lnk" href={releasePage(version)} target="_blank" rel="noreferrer noopener">
              {t("versions.viewOnGitHub")}
            </a>
          </span>
        </div>
      )}
    </section>
  );
}
