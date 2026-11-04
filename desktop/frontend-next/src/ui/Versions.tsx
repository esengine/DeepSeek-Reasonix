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
  if (days <= 0) return t("common.today");
  if (days === 1) return t("common.yesterday");
  if (days < 30) return t("common.n_days_ago", { n: days });
  return new Date(at).toLocaleDateString();
}

// The phase is the sentence. Verifying gets its own because it is the pause
// after the bar fills, which otherwise reads as a hang on a large artifact.
function say(p: UpdateProgress): string {
  switch (p.phase) {
    case "downloading":
      if (p.stalls) return t("about.download_paused_the_connection_delivered", { n: p.stalls });
      if (p.delta_skipped) {
        return p.total > 0 ? t("about.downloading_the_full_package_got_2", { got: bytes(p.received), all: bytes(p.total) }) : t("about.downloading_the_full_package_got", { got: bytes(p.received) });
      }
      return p.total > 0 ? t("about.downloading_got_all", { got: bytes(p.received), all: bytes(p.total) }) : t("about.downloading_got", { got: bytes(p.received) });
    case "verifying":
      return t("about.verifying_the_signature");
    case "downloaded":
      return t("about.preparing_to_install");
    case "applying":
      return t("about.installing");
    case "authorizing":
      return t("about.waiting_for_permission");
    case "idle":
      // Only reachable in the gap between the click and the first read that
      // sees the move: the panel is already showing this row as going.
      return t("about.starting");
    case "relaunching":
      return t("about.restarting_into_the_new_version");
    case "ready":
      return t("about.downloaded_waiting_for_a_restart");
    case "error":
      return t("about.install_failed");
    case "cancelled":
      return t("about.download_cancelled");
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
    return <p className="acct-note">{t("about.this_is_a_development_build")}</p>;
  }

  if (unread) {
    return (
      <div className="find" data-lvl="warn" role="alert">
        <span className="t">{t("about.version_information_could_not_be")}</span>
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
    return <p className="acct-note">{t("about.loading_versions")}</p>;
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
        <span className="lb">{t(dev ? "about.local_build" : "about.current_version")}</span>
        {hub.pinned && !hub.stalePin && <span className="pin">{t("common.pinned")}</span>}
      </div>

      {/* Severity language is the transcript's: a coloured left rule, no icons.
          Pinned is not a problem, so it is ok-coloured; a stale pin is. */}
      {hub.withdrawn && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("about.v_was_withdrawn", { v: hub.withdrawn.version })}</span>
          <span className="why">
            {hub.withdrawn.reason && t("about.reason_reason", { reason: hub.withdrawn.reason })}
            {hub.newer ? t("about.update_to_v_from_its", { v: hub.latest }) : t("about.pick_a_version_below")}
          </span>
        </div>
      )}
      {hub.err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("about.cannot_reach_the_version_index")}</span>
          <span className="why">{t("about.err_nothing_local_is_affected", { err: hub.err })}</span>
        </div>
      )}
      {failed && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("common.that_did_not_go_through")}</span>
          <span className="why">{failed}</span>
        </div>
      )}
      {hub.pinned && !hub.stalePin && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("about.pinned_to_v_newer_versions", { v: hub.pinned })}</span>
          <span className="why">
            {t("about.a_rollback_pins_the_version")}
            <button className="lnk" data-action="versions.pin" onClick={() => pin("")} disabled={locked}>
              {t("about.unpin")}
            </button>
          </span>
        </div>
      )}
      {hub.stalePin && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("about.pinned_to_pinned_what_is", { pinned: hub.pinned, current: hub.current })}</span>
          <span className="why">
            {t("about.this_pin_holds_nothing_and")}
            <button className="lnk" data-action="versions.pin" onClick={() => pin("")} disabled={busy}>
              {t("about.clear_it_now")}
            </button>
          </span>
        </div>
      )}
      {!hub.err && hub.newer && !hub.pinned && !hub.withdrawn && !ready && !moving && !failure && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("about.v_is_out", { v: hub.latest })}</span>
          <span className="why">{t("about.install_it_from_the_row")}</span>
        </div>
      )}
      {ready && later !== ready && (
        <div className="find" data-lvl={running > 0 ? "warn" : "ok"} role="status">
          <span className="t">
            {running > 0 ? t("about.n_tasks_are_running", { n: running }) : t("about.v_is_downloaded_and_its", { v: ready })}
          </span>
          <span className="why">
            {running > 0
              ? t("about.restarting_now_would_stop_them")
              : t("about.it_takes_effect_when_studio")}
            {readyOlder && t("about.a_session_written_by_a")}
          </span>
          <span className="acts">
            <button className="btn sm" data-primary="" data-action="versions.restart" onClick={() => restart(ready, running > 0)}>
              {running > 0 ? t("about.restart_anyway") : t("about.restart_now")}
            </button>
            <button className="btn sm" data-action="versions.later" onClick={() => { setLater(ready); setRunning(0); }}>
              {t("about.later")}
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
                {t("about.download_the_full_installer")}
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
            <span className="ds">{t(v.current ? "about.running_now" : v.older ? "about.an_earlier_version" : "about.a_newer_version")}</span>
            {/* A row the catalog does not carry has no date. Saying so beats an
                empty column: it is why this version has no download page. */}
            <span className="sc">{v.publishedAt ? when(v.publishedAt) : v.current ? t("about.not_published") : ""}</span>
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
                      {cancelling === v.version ? t("common.stopping") : t("about.cancel_download")}
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
              <span className="sa" data-keep="">{t("about.starting")}</span>
            ) : ready === v.version ? (
              <button className="sa lnk" data-keep="" data-action="versions.restart" onClick={() => { setLater(""); void restart(v.version, false); }}>
                {t("about.restart_to_finish")}
              </button>
            ) : (
              !v.current && (
                <button className="sa lnk" data-action="versions.activate" onClick={() => goTo(v.version)} disabled={locked}>
                  {t(v.older ? "about.roll_back_to_this_version" : "about.install_this_version")}
                </button>
              )
            )}
            {v.current && !hub.pinned && (
              <button className="sa lnk" data-action="versions.pin" onClick={() => pin(v.version)} disabled={locked}>
                {t("about.pin_here")}
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
                {t(openNotes === v.version ? "about.hide_whats_new" : "about.whats_new")}
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
            {t("about.show_n_earlier_versions", { n: hidden })}
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
      <div className="vnotes-lang seg" role="group" aria-label={t("about.language_of_the_notes")}>
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
    <section id={id} className="vnotes" data-action="versions.notes-close" onKeyDown={onKeyDown} aria-label={t("about.whats_new_in_v", { v: version })} aria-busy={loading}>
      {loading && <p className="acct-note">{t("common.loading_whats_new")}</p>}
      {state?.phase === "ok" && <NotesBody markdown={state.markdown} />}
      {state?.phase === "err" && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("about.whats_new_could_not_be")}</span>
          <span className="why">
            {state.why}
            {!state.absent && (
              <button className="lnk" data-action="versions.notes-retry" onClick={retry}>
                {t("common.retry")}
              </button>
            )}
            <a className="lnk" href={releasePage(version)} target="_blank" rel="noreferrer noopener">
              {t("about.view_on_github")}
            </a>
          </span>
        </div>
      )}
    </section>
  );
}
