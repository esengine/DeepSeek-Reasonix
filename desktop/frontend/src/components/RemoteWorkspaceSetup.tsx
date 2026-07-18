import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { ArrowUp, Folder, FolderOpen, Loader2, Plus, Server, X } from "lucide-react";

import { app, onRemoteWorkbenchState } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type {
  RemoteDirectoryView,
  RemoteTargetStatusView,
  RemoteWorkbenchStatusView,
  RemoteWorkspaceBrowseInput,
  RemoteWorkspacePageView,
} from "../lib/types";

const BROWSE_LIMIT = 100;

function errorText(error: unknown): string {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  return String(error);
}

function sameDirectory(left: RemoteDirectoryView, right: RemoteDirectoryView): boolean {
  return left.ref === right.ref;
}

export interface RemoteWorkspaceSetupProps {
  target: RemoteTargetStatusView | null;
}

export function RemoteWorkspaceSetup({ target }: RemoteWorkspaceSetupProps) {
  const t = useT();
  const [workbench, setWorkbench] = useState<RemoteWorkbenchStatusView | null>(null);
  const [page, setPage] = useState<RemoteWorkspacePageView | null>(null);
  const [typedPath, setTypedPath] = useState("");
  const [primary, setPrimary] = useState<RemoteDirectoryView | null>(null);
  const [additional, setAdditional] = useState<RemoteDirectoryView[]>([]);
  const [topicTitle, setTopicTitle] = useState("");
  const [loadingStatus, setLoadingStatus] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [browseError, setBrowseError] = useState("");
  const [createError, setCreateError] = useState("");
  const browseSequence = useRef(0);

  const connected = target?.state === "RemoteConnected";
  const hostId = target?.hostId;

  const browse = useCallback(async (input: RemoteWorkspaceBrowseInput, append = false) => {
    const sequence = ++browseSequence.current;
    setBrowsing(true);
    setBrowseError("");
    try {
      const next = await app.BrowseRemoteWorkspace({ ...input, limit: input.limit ?? BROWSE_LIMIT });
      if (sequence !== browseSequence.current) return;
      setPage((current) => {
        if (!append || !current || current.directory.ref !== next.directory.ref) return next;
        const seen = new Set(current.entries.map((entry) => entry.ref));
        return {
          ...next,
          entries: [...current.entries, ...next.entries.filter((entry) => !seen.has(entry.ref))],
        };
      });
      setTypedPath(next.directory.displayPath);
    } catch (cause) {
      if (sequence === browseSequence.current) setBrowseError(errorText(cause));
    } finally {
      if (sequence === browseSequence.current) setBrowsing(false);
    }
  }, []);

  useEffect(() => {
    browseSequence.current += 1;
    setWorkbench(null);
    setPage(null);
    setTypedPath("");
    setPrimary(null);
    setAdditional([]);
    setTopicTitle("");
    setBrowseError("");
    setCreateError("");
    setCreating(false);

    if (!connected) {
      setLoadingStatus(false);
      setBrowsing(false);
      return;
    }

    let active = true;
    setLoadingStatus(true);
    const offWorkbench = onRemoteWorkbenchState((next) => {
      if (!active || (hostId && next.hostId && next.hostId !== hostId)) return;
      setWorkbench(next);
    });
    void (async () => {
      try {
        const next = await app.RemoteWorkbenchStatus();
        if (!active) return;
        if (hostId && next.hostId && next.hostId !== hostId) {
          throw new Error(t("remote.workspace.hostMismatch"));
        }
        setWorkbench(next);
        if (!next.sessionAttached) await browse({}, false);
      } catch (cause) {
        if (active) setBrowseError(errorText(cause));
      } finally {
        if (active) setLoadingStatus(false);
      }
    })();
    return () => {
      active = false;
      browseSequence.current += 1;
      offWorkbench();
    };
  }, [browse, connected, hostId, t]);

  const submitTypedPath = useCallback((event: FormEvent) => {
    event.preventDefault();
    const path = typedPath.trim();
    if (!path || browsing) return;
    void browse({ typedPath: path });
  }, [browse, browsing, typedPath]);

  const choosePrimary = useCallback(() => {
    if (!page) return;
    setPrimary(page.directory);
    setAdditional((current) => current.filter((entry) => !sameDirectory(entry, page.directory)));
    setCreateError("");
  }, [page]);

  const addCurrentDirectory = useCallback(() => {
    if (!page || (primary && sameDirectory(primary, page.directory))) return;
    setAdditional((current) => current.some((entry) => sameDirectory(entry, page.directory)) ? current : [...current, page.directory]);
    setCreateError("");
  }, [page, primary]);

  const createSession = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    const title = topicTitle.trim();
    if (!primary || !title || creating) return;
    setCreating(true);
    setCreateError("");
    try {
      const next = await app.CreateRemoteWorkspaceSession({
        primaryDirectoryRef: primary.ref,
        additionalDirectoryRefs: additional.map((entry) => entry.ref),
        topicTitle: title,
      });
      setWorkbench(next);
    } catch (cause) {
      setCreateError(errorText(cause));
    } finally {
      setCreating(false);
    }
  }, [additional, creating, primary, topicTitle]);

  if (!connected || workbench?.sessionAttached) return null;

  const currentIsPrimary = Boolean(page && primary && sameDirectory(primary, page.directory));
  const currentIsAdditional = Boolean(page && additional.some((entry) => sameDirectory(entry, page.directory)));

  return (
    <div className="management-modal-backdrop remote-workspace-backdrop">
      <section className="management-modal remote-workspace-modal" role="dialog" aria-labelledby="remote-workspace-title">
        <header className="management-modal__head">
          <div>
            <div className="management-modal__title" id="remote-workspace-title">{t("remote.workspace.title")}</div>
            <div className="management-modal__summary">
              {target?.hostLabel ? t("remote.workspace.host", { host: target.hostLabel }) : t("remote.workspace.description")}
            </div>
          </div>
          <Server size={19} aria-hidden="true" />
        </header>

        <div className="remote-workspace-modal__body">
          <section className="remote-workspace-browser" aria-label={t("remote.workspace.browser")}>
            <form className="remote-workspace-path" onSubmit={submitTypedPath}>
              <label htmlFor="remote-workspace-path">{t("remote.workspace.path")}</label>
              <div>
                <input
                  id="remote-workspace-path"
                  name="remote-workspace-path"
                  value={typedPath}
                  onInput={(event) => setTypedPath(event.currentTarget.value)}
                  placeholder={t("remote.workspace.pathPlaceholder")}
                  autoComplete="off"
                  spellCheck={false}
                />
                <button className="btn btn--small" type="submit" disabled={browsing || !typedPath.trim()}>
                  {browsing ? <Loader2 className="spin" size={13} aria-hidden="true" /> : <FolderOpen size={13} aria-hidden="true" />}
                  {t("remote.workspace.browse")}
                </button>
              </div>
            </form>

            {(browseError || createError) && <div className="banner banner--error remote-workspace-error" role="alert">{browseError || createError}</div>}

            {loadingStatus && !page ? (
              <div className="remote-workspace-loading"><Loader2 className="spin" size={18} aria-hidden="true" />{t("remote.workspace.loading")}</div>
            ) : page ? (
              <>
                <div className="remote-workspace-current">
                  <div>
                    <FolderOpen size={16} aria-hidden="true" />
                    <span title={page.directory.displayPath}>{page.directory.displayPath}</span>
                  </div>
                  <div>
                    <button className="btn btn--small" type="button" disabled={browsing || !page.directory.parentRef} onClick={() => page.directory.parentRef && void browse({ directoryRef: page.directory.parentRef })}>
                      <ArrowUp size={13} aria-hidden="true" />{t("remote.workspace.parent")}
                    </button>
                    <button className="btn btn--small" type="button" disabled={currentIsPrimary} onClick={choosePrimary}>
                      {t(currentIsPrimary ? "remote.workspace.primarySelected" : "remote.workspace.usePrimary")}
                    </button>
                    <button className="btn btn--small" type="button" disabled={currentIsPrimary || currentIsAdditional} onClick={addCurrentDirectory}>
                      <Plus size={13} aria-hidden="true" />{t(currentIsAdditional ? "remote.workspace.additionalAdded" : "remote.workspace.addDirectory")}
                    </button>
                  </div>
                </div>
                <div className="remote-workspace-directory-list" role="list">
                  {page.entries.length === 0 && <div className="remote-workspace-empty">{t("remote.workspace.empty")}</div>}
                  {page.entries.map((entry) => (
                    <button key={entry.ref} type="button" role="listitem" onClick={() => void browse({ directoryRef: entry.ref })} disabled={browsing}>
                      <Folder size={16} aria-hidden="true" />
                      <span><strong>{entry.name}</strong><small>{entry.displayPath}</small></span>
                    </button>
                  ))}
                </div>
                {page.hasMore && page.next && (
                  <button className="btn btn--small remote-workspace-load-more" type="button" disabled={browsing} onClick={() => void browse({ directoryRef: page.directory.ref, cursor: page.next }, true)}>
                    {browsing && <Loader2 className="spin" size={13} aria-hidden="true" />}{t("remote.workspace.loadMore")}
                  </button>
                )}
              </>
            ) : (
              <button className="btn btn--small remote-workspace-retry" type="button" disabled={browsing} onClick={() => void browse({})}>{t("common.retry")}</button>
            )}
          </section>

          <form className="remote-workspace-selection" onSubmit={createSession}>
            <div>
              <h3>{t("remote.workspace.selection")}</h3>
              <p>{t("remote.workspace.selectionHint")}</p>
            </div>
            <div className="remote-workspace-selected">
              <span>{t("remote.workspace.primary")}</span>
              {primary ? <code title={primary.displayPath}>{primary.displayPath}</code> : <em>{t("remote.workspace.primaryEmpty")}</em>}
            </div>
            <div className="remote-workspace-selected">
              <span>{t("remote.workspace.additional")}</span>
              {additional.length === 0 ? <em>{t("remote.workspace.additionalEmpty")}</em> : (
                <ul>
                  {additional.map((entry) => (
                    <li key={entry.ref}>
                      <code title={entry.displayPath}>{entry.displayPath}</code>
                      <button type="button" aria-label={t("remote.workspace.removeDirectory", { path: entry.displayPath })} onClick={() => setAdditional((current) => current.filter((candidate) => !sameDirectory(candidate, entry)))}>
                        <X size={13} aria-hidden="true" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <label className="remote-workspace-topic">
              <span>{t("remote.workspace.topic")}</span>
              <input value={topicTitle} onInput={(event) => setTopicTitle(event.currentTarget.value)} placeholder={t("remote.workspace.topicPlaceholder")} autoComplete="off" />
            </label>
            <div className="remote-workspace-create">
              <button className="btn btn--primary" type="submit" disabled={creating || !primary || !topicTitle.trim()}>
                {creating && <Loader2 className="spin" size={14} aria-hidden="true" />}{t("remote.workspace.create")}
              </button>
            </div>
          </form>
        </div>
      </section>
    </div>
  );
}

export default RemoteWorkspaceSetup;
