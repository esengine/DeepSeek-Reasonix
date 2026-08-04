import { Activity, AlertTriangle, ArchiveRestore, Check, ChevronDown, ChevronRight, FileText, History, Pencil, Plus, RefreshCw, Search, Sparkles, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { MemoryArchive, MemoryFact, MemorySuggestion, MemorySuggestionsView, MemoryView, SkillSuggestion, TabMeta } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";
import { ModalCloseButton } from "./ModalCloseButton";

type LinkInfo = {
  name: string;
  exists: boolean;
};

function displayTitle(fact: MemoryFact): string {
  return fact.title || fact.name.replaceAll("-", " ");
}

function memoryFactKey(fact: MemoryFact): string {
  return fact.id || `${fact.scope}:${fact.name}`;
}

function formatMemoryTime(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function freshnessLabel(value: string, t: ReturnType<typeof useT>): string {
  switch (value) {
    case "fresh": return t("memory.freshness.fresh");
    case "current": return t("memory.freshness.current");
    case "stale": return t("memory.freshness.stale");
    default: return value;
  }
}

function memoryMatches(fact: MemoryFact, normalizedQuery: string, typeFilter: string): boolean {
  if (typeFilter !== "all" && fact.type !== typeFilter) return false;
  if (!normalizedQuery) return true;
  return [displayTitle(fact), fact.name, fact.description, fact.type, fact.scope, fact.body]
    .join(" ")
    .toLowerCase()
    .includes(normalizedQuery);
}

function archiveKey(fact: MemoryArchive): string {
  return `${fact.path || fact.name}:${fact.archivedAt || ""}`;
}

function formatArchivedAt(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function ArchivedMemoryList({
  archives,
  totalArchives,
  expanded,
  setExpanded,
  renderWithLinks,
  t,
  hideHeader = false,
  busy = false,
  onRestore,
}: {
  archives: MemoryArchive[];
  totalArchives: number;
  expanded: string | null;
  setExpanded: (key: string | null) => void;
  renderWithLinks: (text: string) => ReactNode[];
  t: ReturnType<typeof useT>;
  hideHeader?: boolean;
  busy?: boolean;
  onRestore?: (archive: MemoryArchive) => Promise<void> | void;
}) {
  if (totalArchives === 0) return null;
  return (
    <div className="mem-archive-block">
      {!hideHeader && <div className="mem-section__row">
        <div>
          <div className="mem-section__title">{t("memory.archivedMemories")}</div>
          <div className="mem-note">{t("memory.archivedHint")}</div>
        </div>
        <span className="mem-count">{totalArchives}</span>
      </div>}
      {archives.length === 0 ? (
        <div className="mem-empty">{t("memory.noArchivedMatches")}</div>
      ) : (
        <div className="mem-facts mem-facts--archive">
          {archives.map((f) => {
            const key = archiveKey(f);
            const isOpen = expanded === key;
            return (
              <article className="mem-fact mem-fact--archived" data-mem-type={f.type || "other"} key={key}>
                <button
                  className="mem-fact__summary"
                  onClick={() => setExpanded(isOpen ? null : key)}
                  type="button"
                >
                  {isOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
                  <span className="mem-fact__main">
                    <span className="mem-fact__title">{displayTitle(f)}</span>
                    <span className="mem-fact__meta">
                      <MemoryFactScope scope={f.scope} t={t} />
                      {f.type && <span className="mem-fact__type" data-mem-type={f.type}>{memoryTypeLabel(f.type, t)}</span>}
                      <span className="mem-fact__slug">{f.name}</span>
                      {f.archivedAt && (
                        <span className="mem-fact__archived">
                          {t("memory.archivedAt", { time: formatArchivedAt(f.archivedAt) })}
                        </span>
                      )}
                    </span>
                    <span className="mem-fact__desc">{f.description}</span>
                  </span>
                </button>
                {isOpen && (
                  <div className="mem-fact__detail">
                    {f.body ? (
                      <div className="mem-fact__body">{renderWithLinks(f.body)}</div>
                    ) : (
                      <div className="mem-empty">{t("memory.noBody")}</div>
                    )}
                    <div className="mem-archive__path">{f.path}</div>
                    {onRestore && (
                      <div className="mem-fact__actions">
                        <span className="mem-hint mem-hint--inline">{t("memory.restoreArchivedHint")}</span>
                        <button
                          className="btn btn--small"
                          type="button"
                          disabled={busy}
                          onClick={() => void onRestore(f)}
                        >
                          <ArchiveRestore size={13} />
                          {t("memory.restoreArchived")}
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </article>
            );
          })}
        </div>
      )}
    </div>
  );
}

function uniqueLinks(body: string, names: Set<string>): LinkInfo[] {
  const links: LinkInfo[] = [];
  const seen = new Set<string>();
  const re = /\[\[([^\]]+)\]\]/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(body)) !== null) {
    const name = match[1].trim();
    if (!name || seen.has(name)) continue;
    seen.add(name);
    links.push({ name, exists: names.has(name) });
  }
  return links;
}

function memoryScopeLabel(scope: string, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "project":
      return t("memory.scope.project");
    case "global":
      return t("memory.scope.global");
    case "user":
      return t("memory.scope.user");
    case "local":
      return t("memory.scope.local");
    case "ancestor":
      return t("memory.scope.ancestor");
    default:
      return scope;
  }
}

function MemoryFactScope({ scope, t }: { scope: string; t: ReturnType<typeof useT> }) {
  if (!scope) return null;
  return <span className="mem-fact__scope" data-mem-scope={scope}>{memoryScopeLabel(scope, t)}</span>;
}

function memoryTypeLabel(type: string, t: ReturnType<typeof useT>): string {
  switch ((type || "").toLowerCase()) {
    case "project":
      return t("memory.type.project");
    case "user":
      return t("memory.type.user");
    case "feedback":
      return t("memory.type.feedback");
    case "reference":
      return t("memory.type.reference");
    default:
      return type || t("memory.type.other");
  }
}

function memoryDocTitle(scope: string, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "project":
      return t("memory.doc.projectTitle");
    case "user":
      return t("memory.doc.userTitle");
    case "local":
      return t("memory.doc.localTitle");
    case "ancestor":
      return t("memory.doc.ancestorTitle");
    default:
      return t("memory.doc.customTitle");
  }
}

function memoryDocHint(scope: string, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "project":
      return t("memory.doc.projectHint");
    case "user":
      return t("memory.doc.userHint");
    case "local":
      return t("memory.doc.localHint");
    case "ancestor":
      return t("memory.doc.ancestorHint");
    default:
      return t("memory.doc.customHint");
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err || "Unknown error");
}

function suggestionTotal(view: MemorySuggestionsView | null): number {
  return (view?.memories?.length ?? 0) + (view?.skills?.length ?? 0);
}

function suggestionStamp(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

// MemoryPanel is the desktop memory manager: a right-side drawer over the loaded
// REASONIX.md hierarchy and saved auto-memories. Unlike Claude Code's /memory
// (which shells out to $EDITOR) it edits docs in place, and unlike Codex (no UI
// at all) it shows the saved facts. Docs are editable; facts are read-only
// (the model owns them via the `remember` tool). Quick-add mirrors the "#"
// shortcut with an explicit scope selector.
export function MemoryPanel({
  view,
  onClose,
  onRemember,
  onForget,
  onSaveDoc,
}: {
  view: MemoryView | null;
  onClose: () => void;
  onRemember: (scope: string, note: string) => Promise<void> | void;
  onForget: (name: string) => Promise<void> | void;
  onSaveDoc: (path: string, body: string) => Promise<void> | void;
}) {
  const t = useT();
  const [note, setNote] = useState("");
  const [scope, setScope] = useState("");
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);

  const [highlight, setHighlight] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState("all");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [expandedArchive, setExpandedArchive] = useState<string | null>(null);
  const [confirmForget, setConfirmForget] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const factRefs = useRef<Record<string, HTMLElement | null>>({});

  // Filter input — a single substring search across docs and facts. The
  // substring is case-insensitive and matches anywhere in the body or the
  // path; an empty string shows everything. The filter is purely frontend
  // (no kernel round-trip) so it's instant and reversible.
  const [filter, setFilter] = useState("");

  const facts = view?.facts ?? [];
  const archives = view?.archives ?? [];
  const factNames = useMemo(() => new Set(facts.map((f) => f.name)), [facts]);
  const factTypes = useMemo(
    () => Array.from(new Set([...facts, ...archives].map((f) => f.type).filter(Boolean))).sort(),
    [facts, archives],
  );
  const normalizedQuery = query.trim().toLowerCase();
  const normalizedFilter = filter.trim().toLowerCase();
  const filteredFacts = useMemo(
    () =>
      facts.filter((f) => {
        if (normalizedFilter) {
          const hay = [f.name, f.description, f.body].join(" ").toLowerCase();
          if (!hay.includes(normalizedFilter)) return false;
        }
        return memoryMatches(f, normalizedQuery, typeFilter);
      }),
    [facts, normalizedQuery, normalizedFilter, typeFilter],
  );
  const filteredArchives = useMemo(
    () =>
      archives.filter((f) => {
        if (normalizedFilter) {
          const hay = [f.name, f.description, f.body, f.path].join(" ").toLowerCase();
          if (!hay.includes(normalizedFilter)) return false;
        }
        return memoryMatches(f, normalizedQuery, typeFilter);
      }),
    [archives, normalizedQuery, normalizedFilter, typeFilter],
  );

  const scrollToFact = (key: string) => {
    const el = factRefs.current[key];
    if (!el) return;
    el.scrollIntoView({ block: "center", behavior: "auto" });
    setHighlight(key);
    window.setTimeout(() => setHighlight((h) => (h === key ? null : h)), 1200);
  };

  // Clear active filters when the target is hidden, else the [[link]] is a silent no-op.
  const jumpTo = (name: string) => {
    if (!factNames.has(name)) return;
    const target = facts.find((f) => f.name === name && f.scope === "project") ?? facts.find((f) => f.name === name);
    if (!target) return;
    const key = memoryFactKey(target);
    const visible = filteredFacts.some((f) => memoryFactKey(f) === key);
    setExpanded(key);
    setConfirmForget(null);
    if (!visible) {
      setQuery("");
      setTypeFilter("all");
      window.setTimeout(() => scrollToFact(key), 0);
      return;
    }
    scrollToFact(key);
  };

  // renderWithLinks turns [[name]] tokens into in-panel jumps; a token with no
  // matching saved memory renders as a flagged dead link.
  const renderWithLinks = (text: string): ReactNode[] => {
    const out: ReactNode[] = [];
    const re = /\[\[([^\]]+)\]\]/g;
    let last = 0;
    let k = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
      if (m.index > last) out.push(text.slice(last, m.index));
      const target = m[1].trim();
      out.push(
        factNames.has(target) ? (
          <button key={k++} type="button" className="mem-link" onClick={() => jumpTo(target)}>
            {target}
          </button>
        ) : (
          <Tooltip key={k++} label={t("memory.deadLink", { name: target })}>
            <span className="mem-link mem-link--dead">{target}</span>
          </Tooltip>
        ),
      );
      last = re.lastIndex;
    }
    if (last < text.length) out.push(text.slice(last));
    return out;
  };

  const forgetFact = async (ref: string) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await onForget(ref);
      if (expanded === ref) setExpanded(null);
      setConfirmForget(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const filteredDocs = useMemo(() => {
    if (!view) return [];
    const q = filter.trim().toLowerCase();
    if (!q) return view.docs;
    return view.docs.filter((d) => d.body.toLowerCase().includes(q) || d.path.toLowerCase().includes(q));
  }, [view, filter]);

  const scopes = view?.scopes ?? [];
  // Default the scope selector to "project" when present, else the first option.
  const activeScope =
    scope || scopes.find((s) => s.scope === "project")?.scope || scopes[0]?.scope || "project";

  const submitNote = async () => {
    const trimmed = note.trim();
    if (!trimmed || busy) return;
    setBusy(true);
    setError(null);
    try {
      await onRemember(activeScope, trimmed);
      setNote("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const startEdit = (path: string, body: string) => {
    setEditingPath(path);
    setDraft(body);
  };

  const saveEdit = async () => {
    if (editingPath === null || busy) return;
    setBusy(true);
    setError(null);
    try {
      await onSaveDoc(editingPath, draft);
      setEditingPath(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <ResizableDrawer onClose={onClose}>
        <header className="drawer__head">
          <div>
            <div className="drawer__title">{t("memory.title")}</div>
            {view?.available && (
              <div className="drawer__summary">
                {t("memory.summary", { facts: facts.length, archives: archives.length, docs: view.docs.length })}
              </div>
            )}
          </div>
          <ModalCloseButton label={t("common.close")} onClick={onClose} />
        </header>

        {!view?.available ? (
          <div className="empty">{t("memory.unavailable")}</div>
        ) : (
          <div className="drawer__body">
            {/* Saved auto-memories — the model owns these via remember/forget;
                the panel can delete one and follow [[name]] cross-links. */}
            <section className="mem-section">
              <div className="mem-section__row">
                <div>
                  <div className="mem-section__title">{t("memory.savedMemories")}</div>
                  <div className="mem-note">{t("memory.fallibleNote")}</div>
                </div>
                <span className="mem-count">{facts.length}</span>
              </div>
              <div className="mem-toolbar">
                <label className="mem-search">
                  <Search size={14} />
                  <input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder={t("memory.searchPlaceholder")}
                  />
                </label>
                <div className="mem-filter" role="tablist" aria-label={t("memory.typeFilter")}>
                  <button
                    className={`mem-filter__item${typeFilter === "all" ? " mem-filter__item--on" : ""}`}
                    onClick={() => setTypeFilter("all")}
                    type="button"
                  >
                    {t("memory.allTypes")}
                  </button>
                  {factTypes.map((type) => (
                    <button
                      className={`mem-filter__item${typeFilter === type ? " mem-filter__item--on" : ""}`}
                      onClick={() => setTypeFilter(type)}
                      type="button"
                      key={type}
                    >
                      {memoryTypeLabel(type, t)}
                    </button>
                  ))}
                </div>
              </div>
              {error && <div className="mem-error" role="alert">{error}</div>}
              {facts.length === 0 ? (
                <div className="mem-empty">{t("memory.noFacts")}</div>
              ) : filteredFacts.length === 0 ? (
                <div className="mem-empty">
                  {t("memory.noMatches")}
                  <button
                    className="mem-empty__action"
                    onClick={() => {
                      setQuery("");
                      setTypeFilter("all");
                    }}
                    type="button"
                  >
                    {t("memory.clearFilters")}
                  </button>
                </div>
              ) : (
                <div className="mem-facts">
                  {filteredFacts.map((f) => {
                    const key = memoryFactKey(f);
                    const isOpen = expanded === key;
                    const links = uniqueLinks(f.body, factNames);
                    const missing = links.filter((link) => !link.exists);
                    return (
                      <article
                        className={`mem-fact${highlight === key ? " mem-fact--hl" : ""}`}
                        data-mem-type={f.type || "other"}
                        key={key}
                        ref={(el) => {
                          factRefs.current[key] = el;
                        }}
                      >
                        <button
                          className="mem-fact__summary"
                          onClick={() => {
                            setExpanded(isOpen ? null : key);
                            setConfirmForget(null);
                          }}
                          type="button"
                        >
                          {isOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
                          <span className="mem-fact__main">
                            <span className="mem-fact__title">{displayTitle(f)}</span>
                            <span className="mem-fact__meta">
                              <MemoryFactScope scope={f.scope} t={t} />
                              {f.type && <span className="mem-fact__type" data-mem-type={f.type}>{memoryTypeLabel(f.type, t)}</span>}
                              <span className="mem-fact__slug">{f.name}</span>
                            </span>
                            <span className="mem-fact__desc">{f.description}</span>
                          </span>
                        </button>
                        {links.length > 0 && (
                          <div className="mem-fact__links" aria-label={t("memory.links")}>
                            {links.map((link) =>
                              link.exists ? (
                                <button
                                  className="mem-link-chip"
                                  key={link.name}
                                  onClick={() => jumpTo(link.name)}
                                  type="button"
                                >
                                  [[{link.name}]]
                                </button>
                              ) : (
                                <Tooltip key={link.name} label={t("memory.deadLink", { name: link.name })}>
                                  <span className="mem-link-chip mem-link-chip--dead">[[{link.name}]]</span>
                                </Tooltip>
                              ),
                            )}
                          </div>
                        )}
                        {isOpen && (
                          <div className="mem-fact__detail">
                            {f.body ? (
                              <div className="mem-fact__body">{renderWithLinks(f.body)}</div>
                            ) : (
                              <div className="mem-empty">{t("memory.noBody")}</div>
                            )}
                            {missing.length > 0 && (
                              <div className="mem-deadline">
                                {t("memory.missingLinks", { n: missing.length })}
                              </div>
                            )}
                            <div className="mem-fact__actions">
                              <span className="mem-hint mem-hint--inline">
                                {t("memory.appliesNow")}
                              </span>
                              {confirmForget === key ? (
                                <div className="mem-confirm">
                                  <button
                                    className="btn btn--small"
                                    onClick={() => setConfirmForget(null)}
                                    disabled={busy}
                                    type="button"
                                  >
                                    {t("common.cancel")}
                                  </button>
                                  <button
                                    className="btn btn--small mem-danger"
                                    onClick={() => void forgetFact(f.id || f.name)}
                                    disabled={busy}
                                    type="button"
                                  >
                                    {t("memory.confirmForget")}
                                  </button>
                                </div>
                               ) : (
                                <button
                                  className="btn btn--small mem-fact__forget"
                                  onClick={() => setConfirmForget(key)}
                                  disabled={busy}
                                  type="button"
                                >
                                  <Trash2 size={13} />
                                  {t("memory.forget")}
                                </button>
                              )}
                            </div>
                          </div>
                        )}
                      </article>
                    );
                  })}
                </div>
              )}
              {(view.storeDir || view.storeGlobalDir) && (
                <div className="mem-hint">{t("memory.storedUnder", { dir: [view.storeDir, view.storeGlobalDir].filter(Boolean).join(" + ") })}</div>
              )}
            </section>

            {archives.length > 0 && <section className="mem-section">
              <ArchivedMemoryList
                archives={filteredArchives}
                totalArchives={archives.length}
                expanded={expandedArchive}
                setExpanded={setExpandedArchive}
                renderWithLinks={renderWithLinks}
                t={t}
              />
            </section>}

            {/* Quick-add: scope selector + note, mirroring the "#" shortcut. */}
            <section className="mem-section">
              <div className="mem-section__title">{t("memory.quickAdd")}</div>
              <div className="mem-add">
                <Tooltip label={t("memory.whereToSave")}>
                  <select
                    className="mem-select"
                    value={activeScope}
                    onChange={(e) => setScope(e.target.value)}
                  >
                    {scopes.map((s) => (
                      <option key={s.scope} value={s.scope}>
                        {s.scope}
                      </option>
                    ))}
                  </select>
                </Tooltip>
                <input
                  className="mem-input"
                  placeholder={t("memory.notePlaceholder")}
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void submitNote();
                  }}
                />
                <button
                  className="btn btn--primary btn--small"
                  onClick={() => void submitNote()}
                  disabled={busy || !note.trim()}
                >
                  {t("memory.remember")}
                </button>
              </div>
              <div className="mem-hint">
                {scopes.find((s) => s.scope === activeScope)?.path}
              </div>
            </section>

            {/* Doc files — editable in place. */}
            <section className="mem-section">
              <div className="mem-section__title">{t("memory.instructionFiles")}</div>
              <input
                className="mem-input mem-filter"
                placeholder={t("memory.filterPlaceholder")}
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                spellCheck={false}
                aria-label={t("memory.filterPlaceholder")}
              />
              {filteredDocs.length === 0 && (
                <div className="mem-empty">{filter ? t("memory.noFilterMatch") : t("memory.noDocs")}</div>
              )}
              {filteredDocs.map((d) => {
                const editing = editingPath === d.path;
                return (
                  <div className="mem-doc" data-doc-scope={d.scope || "other"} key={d.path}>
                    <div className="mem-doc__head">
                      <span className="mem-doc__icon"><FileText size={15} /></span>
                      <span className="mem-doc__info">
                        <span className="mem-doc__name">{memoryDocTitle(d.scope, t)}</span>
                        <span className="mem-doc__path">{d.path}</span>
                      </span>
                      <span className={`mem-doc__tag badge--${d.scope}`}>{memoryScopeLabel(d.scope, t)}</span>
                      {!editing && (
                        <button
                          className="btn btn--small"
                          onClick={() => startEdit(d.path, d.body)}
                        >
                          {t("common.edit")}
                        </button>
                      )}
                    </div>
                    {editing ? (
                      <div className="mem-doc__edit">
                        <textarea
                          className="mem-textarea"
                          value={draft}
                          onChange={(e) => setDraft(e.target.value)}
                          spellCheck={false}
                        />
                        <div className="mem-doc__actions">
                          <button
                            className="btn btn--small"
                            onClick={() => setEditingPath(null)}
                            disabled={busy}
                          >
                            {t("common.cancel")}
                          </button>
                          <button
                            className="btn btn--primary btn--small"
                            onClick={() => void saveEdit()}
                            disabled={busy}
                          >
                            {t("common.save")}
                          </button>
                        </div>
                      </div>
                    ) : (
                      <pre className="mem-doc__body">{d.body}</pre>
                    )}
                  </div>
                );
              })}
            </section>



            {/* Saved auto-memories — read-only; the model owns these. */}
            <section className="mem-section">
              <div className="mem-section__title">{t("memory.savedMemories")}</div>
              {filteredFacts.length === 0 ? (
                <div className="mem-empty">{filter ? t("memory.noFilterMatch") : t("memory.noFacts")}</div>
              ) : (
                filteredFacts.map((f) => (
                  <div className="mem-fact" key={memoryFactKey(f)} title={f.body}>
                    <span className={`badge badge--${f.scope}`}>{memoryScopeLabel(f.scope, t)}</span>
                    <span className={`badge badge--${f.type}`}>{memoryTypeLabel(f.type, t)}</span>
                    <div className="mem-fact__text">
                      <div className="mem-fact__name">{f.name}</div>
                      <div className="mem-fact__desc">{f.description}</div>
                    </div>
                  </div>
                ))
              )}
              {(view.storeDir || view.storeGlobalDir) && (
                <div className="mem-hint" title={[view.storeDir, view.storeGlobalDir].filter(Boolean).join(" + ")}>
                  {t("memory.storedUnder", { dir: [view.storeDir, view.storeGlobalDir].filter(Boolean).join(" + ") })}
                </div>
              )}
            </section>
          </div>
        )}
    </ResizableDrawer>
  );
}

// MemorySettingsPage is a self-contained memory management page embedded inside
// the settings centre. It loads its own data and handles all memory operations.
export function MemorySettingsPage() {
	const t = useT();
	const [view, setView] = useState<MemoryView | null>(null);
	const [tabs, setTabs] = useState<TabMeta[]>([]);
	const [selectedTabId, setSelectedTabId] = useState<string | null>(null);
	const [note, setNote] = useState("");
	const [scope, setScope] = useState("");
	const [editingPath, setEditingPath] = useState<string | null>(null);
	const [draft, setDraft] = useState("");
	const [busy, setBusy] = useState(false);
	const [highlight, setHighlight] = useState<string | null>(null);
	const [query, setQuery] = useState("");
	const [typeFilter, setTypeFilter] = useState("all");
	const [expanded, setExpanded] = useState<string | null>(null);
	const [expandedArchive, setExpandedArchive] = useState<string | null>(null);
	const [expandedDoc, setExpandedDoc] = useState<string | null>(null);
	const [confirmForget, setConfirmForget] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [tab, setTab] = useState<"saved" | "archived" | "docs" | "activity" | "suggestions">("saved");
	const [showAdd, setShowAdd] = useState(false);
	const [showStorage, setShowStorage] = useState(false);
	const [suggestions, setSuggestions] = useState<MemorySuggestionsView | null>(null);
	const [suggestionBusy, setSuggestionBusy] = useState(false);
	const [expandedSuggestion, setExpandedSuggestion] = useState<string | null>(null);
	const [acceptedSuggestions, setAcceptedSuggestions] = useState<Record<string, string>>({});
	const [revisions, setRevisions] = useState<Record<string, MemoryFact[]>>({});
	const [revisionBusy, setRevisionBusy] = useState<string | null>(null);
	const factRefs = useRef<Record<string, HTMLElement | null>>({});

	useEffect(() => {
		app.ListTabs().then((tabList) => {
			setTabs(tabList);
			if (!selectedTabId) {
				const active = tabList.find((tb) => tb.active);
				if (active) setSelectedTabId(active.id);
			}
		}).catch(() => {});
	}, []);

	// Deduplicate tabs by workspace: multiple conversations in the same project
	// should appear as a single entry in the memory workspace selector.
	const uniqueWorkspaceTabs = useMemo(() => {
		const byWorkspace = new Map<string, TabMeta>();
		for (const tb of tabs) {
			const key = tb.workspaceRoot || `${tb.scope}:global`;
			if (!byWorkspace.has(key)) byWorkspace.set(key, tb);
		}
		return [...byWorkspace.values()];
	}, [tabs]);

	// Ensure selectedTabId always points to a valid entry in uniqueWorkspaceTabs.
	// On initial load the active tab is picked; if dedup removed it, fall back to first.
	const effectiveTabId = useMemo(() => {
		if (uniqueWorkspaceTabs.some((tb) => tb.id === selectedTabId)) return selectedTabId;
		return uniqueWorkspaceTabs[0]?.id ?? null;
	}, [selectedTabId, uniqueWorkspaceTabs]);

	// Sync effectiveTabId back to selectedTabId when it changes
	useEffect(() => {
		if (effectiveTabId && effectiveTabId !== selectedTabId) {
			setSelectedTabId(effectiveTabId);
		}
	}, [effectiveTabId]);

	const reload = useCallback(async () => {
		const tabId = effectiveTabId;
		// Clear view immediately so stale data from the previous workspace
		// doesn't persist while the new workspace loads.
		setView((prev) => {
			if (prev && tabId) return {
				...prev,
				facts: [], archives: [], docs: [], conflicts: [], instructionDiagnostics: [],
				lastRecall: { query: "", hits: [], omitted: 0, charBudget: 0, usedChars: 0 },
			};
			return prev;
		});
		setView(tabId ? await app.MemoryForTab(tabId).catch(() => null) : await app.Memory().catch(() => null));
	}, [effectiveTabId]);

	useEffect(() => { void reload(); }, [reload]);
	useEffect(() => {
		setRevisions({});
		setExpanded(null);
		setExpandedArchive(null);
		setExpandedDoc(null);
		setSuggestions(null);
	}, [effectiveTabId]);

	// Workspace selector: custom styled dropdown matching settings-subtab height
	const wsTriggerRef = useRef<HTMLButtonElement>(null);
	const [wsOpen, setWsOpen] = useState(false);
	const selectedWs = uniqueWorkspaceTabs.find((tb) => tb.id === effectiveTabId);

	const wsSelector = uniqueWorkspaceTabs.length > 0 ? (
		<div className="mem-ws-select">
			{uniqueWorkspaceTabs.length > 1 ? (
				<>
					<button
						ref={wsTriggerRef}
						type="button"
						className="mem-ws-select__trigger"
						onClick={() => setWsOpen((v) => !v)}
					>
						<span className="mem-ws-select__label">{selectedWs?.workspaceName || selectedWs?.label || ""}</span>
						<ChevronDown size={13} className={"mem-ws-select__chev" + (wsOpen ? " mem-ws-select__chev--open" : "")} />
					</button>
					<AnchoredPopover
						open={wsOpen}
						anchorRef={wsTriggerRef}
						onClose={() => setWsOpen(false)}
						className="mem-ws-select__menu"
						placement="bottom"
					>
						<div className="mem-ws-select__list" role="listbox">
							{uniqueWorkspaceTabs.map((tb) => (
								<button
									key={tb.id}
									type="button"
									role="option"
									aria-selected={tb.id === effectiveTabId}
									className={"mem-ws-select__option" + (tb.id === effectiveTabId ? " mem-ws-select__option--selected" : "")}
									onClick={() => { setSelectedTabId(tb.id); setWsOpen(false); }}
								>
									<span>{tb.workspaceName || tb.label || tb.scope || tb.id}</span>
									{tb.id === effectiveTabId && <Check size={13} />}
								</button>
							))}
						</div>
					</AnchoredPopover>
				</>
			) : (
				<span className="mem-ws-select__label mem-ws-select__label--single">{selectedWs?.workspaceName || selectedWs?.label || ""}</span>
			)}
		</div>
	) : null;

	const refreshSuggestions = useCallback(async () => {
		if (suggestionBusy) return;
		setSuggestionBusy(true);
		setError(null);
		try {
			const next = effectiveTabId
				? await app.MemorySuggestionsForTab(effectiveTabId)
				: await app.MemorySuggestions();
			setSuggestions({
				memories: next.memories ?? [],
				skills: next.skills ?? [],
				generatedAt: next.generatedAt || "",
				available: !!next.available,
				source: next.source || "",
			});
			setAcceptedSuggestions({});
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setSuggestionBusy(false);
		}
	}, [effectiveTabId, suggestionBusy]);

	useEffect(() => {
		if (tab !== "suggestions" || suggestions || suggestionBusy) return;
		void refreshSuggestions();
	}, [refreshSuggestions, suggestionBusy, suggestions, tab]);

	const facts = view?.facts ?? [];
	const archives = view?.archives ?? [];
	const factNames = useMemo(() => new Set(facts.map((f) => f.name)), [facts]);
	const factTypes = useMemo(
		() => Array.from(new Set([...facts, ...archives].map((f) => f.type).filter(Boolean))).sort(),
		[facts, archives],
	);
	const normalizedQuery = query.trim().toLowerCase();
	const filteredFacts = useMemo(
		() =>
			facts.filter((f) => memoryMatches(f, normalizedQuery, typeFilter)),
		[facts, normalizedQuery, typeFilter],
	);
	const filteredArchives = useMemo(
		() =>
			archives.filter((f) => {
				if (typeFilter !== "all" && f.type !== typeFilter) return false;
				if (!normalizedQuery) return true;
				return memoryMatches(f, normalizedQuery, "all") || [f.path, f.archivedAt].join(" ").toLowerCase().includes(normalizedQuery);
			}),
		[archives, normalizedQuery, typeFilter],
	);

	const scrollToFact = useCallback((key: string) => {
		const el = factRefs.current[key];
		if (!el) return;
		el.scrollIntoView({ block: "center", behavior: "auto" });
		setHighlight(key);
		window.setTimeout(() => setHighlight((h) => (h === key ? null : h)), 1200);
	}, []);

	const jumpTo = useCallback((name: string) => {
		if (!factNames.has(name)) return;
		const target = facts.find((f) => f.name === name && f.scope === "project") ?? facts.find((f) => f.name === name);
		if (!target) return;
		const key = memoryFactKey(target);
		const visible = filteredFacts.some((f) => memoryFactKey(f) === key);
		setExpanded(key);
		setConfirmForget(null);
		if (!visible) {
			setQuery("");
			setTypeFilter("all");
			window.setTimeout(() => scrollToFact(key), 0);
			return;
		}
		scrollToFact(key);
	}, [factNames, facts, filteredFacts, scrollToFact]);

	const renderWithLinks = useCallback((text: string): ReactNode[] => {
		const out: ReactNode[] = [];
		const re = /\[\[([^\]]+)\]\]/g;
		let last = 0;
		let k = 0;
		let m: RegExpExecArray | null;
		while ((m = re.exec(text)) !== null) {
			if (m.index > last) out.push(text.slice(last, m.index));
			const target = m[1].trim();
			out.push(
				factNames.has(target) ? (
					<button key={k++} type="button" className="mem-link" onClick={() => jumpTo(target)}>
						{target}
					</button>
				) : (
					<Tooltip key={k++} label={t("memory.deadLink", { name: target })}>
						<span className="mem-link mem-link--dead">{target}</span>
					</Tooltip>
				),
			);
			last = re.lastIndex;
		}
		if (last < text.length) out.push(text.slice(last));
		return out;
	}, [factNames, jumpTo, t]);

	const forgetFact = useCallback(async (ref: string, key: string) => {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			if (effectiveTabId) await app.ForgetForTab(effectiveTabId, ref);
			else await app.Forget(ref);
			await reload();
			if (expanded === key) setExpanded(null);
			setConfirmForget(null);
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [busy, expanded, reload, effectiveTabId]);

	const loadRevisions = useCallback(async (fact: MemoryFact) => {
		const ref = fact.id || fact.name;
		const key = memoryFactKey(fact);
		if (!ref || revisions[key] || revisionBusy === key) return;
		setRevisionBusy(key);
		try {
			const items = effectiveTabId
				? await app.MemoryRevisionsForTab(effectiveTabId, ref)
				: await app.MemoryRevisions(ref);
			setRevisions((prev) => ({ ...prev, [key]: items ?? [] }));
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setRevisionBusy(null);
		}
	}, [effectiveTabId, revisionBusy, revisions]);

	const restoreRevision = useCallback(async (fact: MemoryFact, revision: number) => {
		const ref = fact.id || fact.name;
		const key = memoryFactKey(fact);
		if (!ref || busy) return;
		setBusy(true);
		setError(null);
		try {
			if (effectiveTabId) await app.RestoreMemoryRevisionForTab(effectiveTabId, ref, revision);
			else await app.RestoreMemoryRevision(ref, revision);
			setRevisions((prev) => {
				const next = { ...prev };
				delete next[key];
				return next;
			});
			await reload();
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [busy, effectiveTabId, reload]);

	const restoreArchive = useCallback(async (archive: MemoryArchive) => {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			const restored = effectiveTabId
				? await app.RestoreArchivedMemoryForTab(effectiveTabId, archive.path)
				: await app.RestoreArchivedMemory(archive.path);
			await reload();
			setExpandedArchive(null);
			setExpanded(restored.name || archive.name);
			setHighlight(restored.name || archive.name);
			setTab("saved");
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [busy, effectiveTabId, reload]);

	const scopes = view?.scopes ?? [];
	const activeScope =
		scope || scopes.find((s) => s.scope === "project")?.scope || scopes[0]?.scope || "project";

	const submitNote = useCallback(async () => {
		const trimmed = note.trim();
		if (!trimmed || busy) return;
		setBusy(true);
		setError(null);
		try {
			if (effectiveTabId) await app.RememberForTab(effectiveTabId, activeScope, trimmed);
			else await app.Remember(activeScope, trimmed);
			await reload();
			setNote("");
			setShowAdd(false);
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [note, busy, activeScope, reload, effectiveTabId]);

	const startEdit = useCallback((path: string, body: string) => {
		setEditingPath(path);
		setDraft(body);
	}, []);

	const saveEdit = useCallback(async () => {
		if (editingPath === null || busy) return;
		setBusy(true);
		setError(null);
		try {
			if (effectiveTabId) await app.SaveDocForTab(effectiveTabId, editingPath, draft);
			else await app.SaveDoc(editingPath, draft);
			await reload();
			setEditingPath(null);
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [editingPath, busy, draft, reload, effectiveTabId]);

	const acceptMemorySuggestion = useCallback(async (candidate: MemorySuggestion) => {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			const path = effectiveTabId
				? await app.AcceptMemorySuggestionForTab(effectiveTabId, candidate)
				: await app.AcceptMemorySuggestion(candidate);
			setAcceptedSuggestions((prev) => ({ ...prev, [candidate.id]: path || candidate.name }));
			await reload();
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [busy, reload, effectiveTabId]);

	const acceptSkillSuggestion = useCallback(async (candidate: SkillSuggestion) => {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			const path = effectiveTabId
				? await app.AcceptSkillSuggestionForTab(effectiveTabId, candidate)
				: await app.AcceptSkillSuggestion(candidate);
			setAcceptedSuggestions((prev) => ({ ...prev, [candidate.id]: path || candidate.name }));
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [busy, effectiveTabId]);

	if (!view?.available) {
		return (
			<>
				{wsSelector}
				<div className="empty">{t("memory.unavailable")}</div>
			</>
		);
	}

	const hasSavedFilters = facts.length > 0;
	const hasArchivedFilters = archives.length > 0;

	return (
		<>
			<div className="memory-overview" aria-label={t("memory.title")}>
				<div className="memory-overview__copy">
					<span>{t("memory.summarySettings", { facts: facts.length, archives: archives.length, docs: view.docs.length })}</span>
				</div>
				{view.storeDir && (
					<button
						className="memory-storage-toggle"
						type="button"
						onClick={() => setShowStorage((v) => !v)}
					>
						{showStorage ? t("memory.hideStorage") : t("memory.showStorage")}
					</button>
				)}
			</div>
			{showStorage && view.storeDir && (
				<div className="memory-storage-path">
					<span>{t("memory.storagePathLabel")}</span>
					<code>{view.storeDir}</code>
				</div>
			)}
			<div className="memory-tabs-row" role="tablist" aria-label={t("settings.tab.memory")}>
				<div className="settings-subtabs memory-tabs-row__primary" role="presentation">
					<button
						className={"settings-subtab" + (tab === "saved" ? " settings-subtab--active" : "")}
						role="tab"
						aria-selected={tab === "saved"}
						type="button"
						onClick={() => setTab("saved")}
					>
						<span>{t("memory.savedMemories")}</span>
					</button>
					<button
						className={"settings-subtab" + (tab === "archived" ? " settings-subtab--active" : "")}
						role="tab"
						aria-selected={tab === "archived"}
						type="button"
						onClick={() => setTab("archived")}
					>
						<span>{t("memory.archivedMemories")}</span>
					</button>
					<button
						className={"settings-subtab" + (tab === "docs" ? " settings-subtab--active" : "")}
						role="tab"
						aria-selected={tab === "docs"}
						type="button"
						onClick={() => setTab("docs")}
					>
						<span>{t("memory.instructionFiles")}</span>
					</button>
					<button
						className={"settings-subtab" + (tab === "activity" ? " settings-subtab--active" : "")}
						role="tab"
						aria-selected={tab === "activity"}
						type="button"
						onClick={() => setTab("activity")}
					>
						<span>{t("memory.activity")}</span>
					</button>
				</div>
				<div className="memory-tabs-row__spacer" />
				<div className="memory-tabs-row__tail" role="presentation">
					{wsSelector}
					<button
						className={"memory-suggestion-tab" + (tab === "suggestions" ? " memory-suggestion-tab--active" : "")}
						role="tab"
						aria-selected={tab === "suggestions"}
						type="button"
						onClick={() => setTab("suggestions")}
					>
						<Sparkles size={14} aria-hidden="true" />
						<span>{t("memory.suggestions")}</span>
						{suggestionTotal(suggestions) > 0 && <span className="settings-subtab__count">{suggestionTotal(suggestions)}</span>}
					</button>
				</div>
			</div>

			{tab === "saved" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.savedMemories")}</div>
						<div className="mem-note">{t("memory.fallibleNote")}</div>
					</div>
				</div>
				{view.conflicts.length > 0 && (
					<div className="mem-context-notice" role="status">
						<AlertTriangle size={15} />
						<div>
							<strong>{t("memory.overridesTitle", { count: view.conflicts.length })}</strong>
							{view.conflicts.map((conflict) => (
								<span key={`${conflict.projectId}:${conflict.globalId}:${conflict.key}`}>
									{t("memory.overrideExplanation", { project: conflict.projectName, global: conflict.globalName })}
								</span>
							))}
						</div>
					</div>
				)}
				{hasSavedFilters && <div className="mem-toolbar">
					<label className="mem-search">
						<Search size={14} />
						<input
							value={query}
							onChange={(e) => setQuery(e.target.value)}
							placeholder={t("memory.searchPlaceholder")}
						/>
					</label>
					<div className="mem-filter" role="tablist" aria-label={t("memory.typeFilter")}>
						<button
							className={"mem-filter__item" + (typeFilter === "all" ? " mem-filter__item--on" : "")}
							onClick={() => setTypeFilter("all")}
							type="button"
						>
							{t("memory.allTypes")}
						</button>
						{factTypes.map((type) => (
							<button
								className={"mem-filter__item" + (typeFilter === type ? " mem-filter__item--on" : "")}
								onClick={() => setTypeFilter(type)}
								type="button"
								key={type}
							>
								{memoryTypeLabel(type, t)}
							</button>
						))}
					</div>
				</div>}
				{error && <div className="mem-error" role="alert">{error}</div>}
				{facts.length === 0 ? (
					<div className="mem-empty mem-empty--cta">
						<strong>{t("memory.emptySavedTitle")}</strong>
						<span>{t("memory.emptySavedBody")}</span>
					</div>
				) : filteredFacts.length === 0 ? (
					<div className="mem-empty">
						{t("memory.noMatches")}
						<button
							className="mem-empty__action"
							onClick={() => {
								setQuery("");
								setTypeFilter("all");
							}}
							type="button"
						>
							{t("memory.clearFilters")}
						</button>
					</div>
				) : (
					<div className="mem-facts">
						{filteredFacts.map((f) => {
							const key = memoryFactKey(f);
							const isOpen = expanded === key;
							const links = uniqueLinks(f.body, factNames);
							const missing = links.filter((link) => !link.exists);
							const factRevisions = revisions[key];
							return (
								<article
									className={"mem-fact" + (highlight === key ? " mem-fact--hl" : "")}
									data-mem-type={f.type || "other"}
									key={key}
									ref={(el) => {
										factRefs.current[key] = el;
									}}
								>
									<button
										className="mem-fact__summary"
										onClick={() => {
											setExpanded(isOpen ? null : key);
											setConfirmForget(null);
											if (!isOpen) void loadRevisions(f);
										}}
										type="button"
									>
										{isOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
										<span className="mem-fact__main">
											<span className="mem-fact__title">{displayTitle(f)}</span>
											<span className="mem-fact__meta">
												<MemoryFactScope scope={f.scope} t={t} />
												{f.type && <span className="mem-fact__type" data-mem-type={f.type}>{memoryTypeLabel(f.type, t)}</span>}
												<span className={`mem-freshness mem-freshness--${f.freshness || "current"}`}>{freshnessLabel(f.freshness, t)}</span>
												<span className="mem-fact__slug">{f.name}</span>
											</span>
											<span className="mem-fact__desc">{f.description}</span>
										</span>
									</button>
									{links.length > 0 && (
										<div className="mem-fact__links" aria-label={t("memory.links")}>
											{links.map((link) =>
												link.exists ? (
													<button
														className="mem-link-chip"
														key={link.name}
														onClick={() => jumpTo(link.name)}
														type="button"
													>
														[[{link.name}]]
													</button>
												) : (
													<Tooltip key={link.name} label={t("memory.deadLink", { name: link.name })}>
														<span className="mem-link-chip mem-link-chip--dead">[[{link.name}]]</span>
													</Tooltip>
												),
											)}
										</div>
									)}
									{isOpen && (
										<div className="mem-fact__detail">
											{f.body ? (
												<div className="mem-fact__body">{renderWithLinks(f.body)}</div>
											) : (
												<div className="mem-empty">{t("memory.noBody")}</div>
											)}
										{missing.length > 0 && (
												<div className="mem-deadline">
													{t("memory.missingLinks", { n: missing.length })}
												</div>
										)}
										<div className="mem-fact__provenance">
											<span>{t("memory.factId")}: <code>{f.id || f.name}</code></span>
											<span>{t("memory.revision", { revision: f.revision || 1 })}</span>
											{f.updatedAt && <span>{t("memory.updatedAt", { time: formatMemoryTime(f.updatedAt) })}</span>}
										</div>
										<div className="mem-revisions">
											<div className="mem-revisions__head"><History size={13} /><strong>{t("memory.revisionHistory")}</strong></div>
											{revisionBusy === key ? (
												<span className="mem-note">{t("memory.loadingRevisions")}</span>
											) : !factRevisions || factRevisions.length === 0 ? (
												<span className="mem-note">{t("memory.noRevisions")}</span>
											) : factRevisions.map((revision) => (
												<div className="mem-revision" key={`${key}:${revision.revision}`}>
													<div>
														<strong>{t("memory.revision", { revision: revision.revision || 1 })}</strong>
														<span>{formatMemoryTime(revision.updatedAt || revision.createdAt)}</span>
													</div>
													<button className="btn btn--small" type="button" disabled={busy} onClick={() => void restoreRevision(f, revision.revision || 1)}>
														<ArchiveRestore size={13} />{t("memory.restoreRevision")}
													</button>
												</div>
											))}
										</div>
										<div className="mem-fact__actions">
												<span className="mem-hint mem-hint--inline">
													{t("memory.appliesNow")}
												</span>
											{confirmForget === key ? (
													<div className="mem-confirm">
														<button
															className="btn btn--small"
															onClick={() => setConfirmForget(null)}
															disabled={busy}
															type="button"
														>
															{t("common.cancel")}
														</button>
														<button
															className="btn btn--small mem-danger"
														onClick={() => void forgetFact(f.id || f.name, key)}
															disabled={busy}
															type="button"
														>
															{t("memory.confirmForget")}
														</button>
													</div>
												) : (
													<button
														className="btn btn--small mem-fact__forget"
													onClick={() => setConfirmForget(key)}
														disabled={busy}
														type="button"
													>
														<Trash2 size={13} />
														{t("memory.forget")}
													</button>
												)}
											</div>
										</div>
									)}
								</article>
							);
						})}
					</div>
				)}
			{(view.storeDir || view.storeGlobalDir) && (
				<div className="mem-hint">{t("memory.storedUnder", { dir: [view.storeDir, view.storeGlobalDir].filter(Boolean).join(" + ") })}</div>
			)}
			</section>}

			{tab === "suggestions" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.suggestions")}</div>
						<div className="mem-note">{t("memory.suggestionsHint")}</div>
					</div>
					<div className="mem-section__actions">
						<button
							className="btn btn--small"
							type="button"
							disabled={suggestionBusy || busy}
							onClick={() => void refreshSuggestions()}
						>
							<RefreshCw size={13} />
							{suggestions ? t("memory.refreshSuggestions") : t("memory.scanSuggestions")}
						</button>
					</div>
				</div>
				{error && <div className="mem-error" role="alert">{error}</div>}
				{!suggestions ? (
					<div className="mem-empty mem-empty--cta">
						<strong>{t("memory.suggestionsEmptyTitle")}</strong>
						<span>{t("memory.suggestionsEmptyBody")}</span>
						<button
							className="btn btn--primary btn--small"
							type="button"
							disabled={suggestionBusy || busy}
							onClick={() => void refreshSuggestions()}
						>
							<Sparkles size={13} />
							{t("memory.scanSuggestions")}
						</button>
					</div>
				) : suggestionTotal(suggestions) === 0 ? (
					<div className="mem-empty mem-empty--cta">
						<strong>{t("memory.noSuggestionsTitle")}</strong>
						<span>{t("memory.noSuggestionsBody")}</span>
					</div>
				) : (
					<div className="mem-suggestions">
						{suggestions.generatedAt && (
							<div className="mem-suggestions__stamp">
								{t("memory.suggestionsGenerated", { time: suggestionStamp(suggestions.generatedAt) })}
							</div>
						)}
						{suggestions.memories.length > 0 && (
							<div className="mem-suggestion-group">
								<div className="mem-suggestion-group__title">{t("memory.memoryCandidates")}</div>
								<div className="mem-facts">
									{suggestions.memories.map((candidate) => {
										const open = expandedSuggestion === candidate.id;
										const accepted = acceptedSuggestions[candidate.id];
										return (
											<article className="mem-fact mem-suggestion" data-mem-type={candidate.type || "other"} key={candidate.id}>
												<button
													className="mem-fact__summary"
													type="button"
													onClick={() => setExpandedSuggestion(open ? null : candidate.id)}
												>
												{open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
												<span className="mem-fact__main">
													<span className="mem-fact__title">{candidate.title || candidate.name}</span>
													<span className="mem-fact__meta">
														<MemoryFactScope scope={candidate.scope} t={t} />
														<span className="mem-fact__type" data-mem-type={candidate.type}>{memoryTypeLabel(candidate.type, t)}</span>
														<span className="mem-fact__slug">{candidate.name}</span>
													</span>
														<span className="mem-fact__desc">{candidate.description}</span>
													</span>
												</button>
												{open && (
													<div className="mem-fact__detail">
														<div className="mem-suggestion__body">{candidate.body}</div>
														{candidate.reason && <div className="mem-suggestion__reason">{candidate.reason}</div>}
														{candidate.evidence.length > 0 && (
															<ul className="mem-suggestion__evidence">
																{candidate.evidence.map((item) => <li key={item}>{item}</li>)}
															</ul>
														)}
														<div className="mem-fact__actions">
															<span className="mem-hint mem-hint--inline">{t("memory.confirmBeforeApply")}</span>
															{accepted ? (
																<span className="mem-suggestion__accepted"><Check size={13} />{t("memory.savedSuggestion")}</span>
															) : (
																<button
																	className="btn btn--primary btn--small"
																	type="button"
																	disabled={busy}
																	onClick={() => void acceptMemorySuggestion(candidate)}
																>
																	<Check size={13} />
																	{t("memory.saveAsMemory")}
																</button>
															)}
														</div>
													</div>
												)}
											</article>
										);
									})}
								</div>
							</div>
						)}
						{suggestions.skills.length > 0 && (
							<div className="mem-suggestion-group">
								<div className="mem-suggestion-group__title">{t("memory.skillCandidates")}</div>
								<div className="mem-facts">
									{suggestions.skills.map((candidate) => {
										const open = expandedSuggestion === candidate.id;
										const accepted = acceptedSuggestions[candidate.id];
										return (
											<article className="mem-fact mem-suggestion mem-suggestion--skill" data-mem-type="reference" key={candidate.id}>
												<button
													className="mem-fact__summary"
													type="button"
													onClick={() => setExpandedSuggestion(open ? null : candidate.id)}
												>
													{open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
													<span className="mem-doc__icon"><FileText size={15} /></span>
													<span className="mem-fact__main">
														<span className="mem-fact__title">{candidate.name}</span>
														<span className="mem-fact__meta">
															<span className="mem-fact__type">{t("memory.skillCandidate")}</span>
															<span className="mem-fact__slug">{memoryScopeLabel(candidate.scope, t)}</span>
														</span>
														<span className="mem-fact__desc">{candidate.description}</span>
													</span>
												</button>
												{open && (
													<div className="mem-fact__detail">
														<pre className="mem-suggestion__body mem-suggestion__body--code">{candidate.body}</pre>
														{candidate.reason && <div className="mem-suggestion__reason">{candidate.reason}</div>}
														{candidate.evidence.length > 0 && (
															<ul className="mem-suggestion__evidence">
																{candidate.evidence.map((item) => <li key={item}>{item}</li>)}
															</ul>
														)}
														<div className="mem-fact__actions">
															<span className="mem-hint mem-hint--inline">{t("memory.confirmBeforeApply")}</span>
															{accepted ? (
																<span className="mem-suggestion__accepted"><Check size={13} />{t("memory.createdSkillSuggestion")}</span>
															) : (
																<button
																	className="btn btn--primary btn--small"
																	type="button"
																	disabled={busy}
																	onClick={() => void acceptSkillSuggestion(candidate)}
																>
																	<Check size={13} />
																	{t("memory.createSkill")}
																</button>
															)}
														</div>
													</div>
												)}
											</article>
										);
									})}
								</div>
							</div>
						)}
					</div>
				)}
			</section>}

			{tab === "activity" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.recallTitle")}</div>
						<div className="mem-note">{t("memory.recallHint")}</div>
					</div>
				</div>
				<div className="mem-recall-summary">
					<Activity size={16} />
					<div>
						<strong>{view.lastRecall.query || t("memory.noRecallQuery")}</strong>
						<span>{t("memory.recallBudget", { used: view.lastRecall.usedChars, budget: view.lastRecall.charBudget, omitted: view.lastRecall.omitted })}</span>
					</div>
				</div>
				{view.lastRecall.suppressed && (
					<div className="mem-context-notice mem-context-notice--muted">
						<AlertTriangle size={15} />
						<span>{t("memory.recallSuppressed", { reason: view.lastRecall.suppressed })}</span>
					</div>
				)}
				{view.lastRecall.hits.length === 0 ? (
					<div className="mem-empty">{t("memory.noRecallHits")}</div>
				) : (
					<div className="mem-recall-hits">
						{view.lastRecall.hits.map((hit) => (
							<div className="mem-recall-hit" key={`${hit.id}:${hit.revision}`}>
								<div className="mem-recall-hit__head">
									<strong>{hit.title || hit.name}</strong>
									<span>{Math.round(hit.score * 100)}%</span>
								</div>
								<div className="mem-fact__meta">
									<MemoryFactScope scope={hit.scope} t={t} />
									<span className="mem-fact__type" data-mem-type={hit.type}>{memoryTypeLabel(hit.type, t)}</span>
									<span className={`mem-freshness mem-freshness--${hit.freshness}`}>{freshnessLabel(hit.freshness, t)}</span>
									<span>{t("memory.revision", { revision: hit.revision })}</span>
								</div>
								<p>{hit.snippet}</p>
								<small>{hit.reason}</small>
							</div>
						))}
					</div>
				)}
			</section>}

			{tab === "archived" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.archivedMemories")}</div>
						<div className="mem-note">{t("memory.archivedHint")}</div>
					</div>
				</div>
				{hasArchivedFilters && <div className="mem-toolbar">
					<label className="mem-search">
						<Search size={14} />
						<input
							value={query}
							onChange={(e) => setQuery(e.target.value)}
							placeholder={t("memory.searchPlaceholder")}
						/>
					</label>
					<div className="mem-filter" role="tablist" aria-label={t("memory.typeFilter")}>
						<button
							className={"mem-filter__item" + (typeFilter === "all" ? " mem-filter__item--on" : "")}
							onClick={() => setTypeFilter("all")}
							type="button"
						>
							{t("memory.allTypes")}
						</button>
						{factTypes.map((type) => (
							<button
								className={"mem-filter__item" + (typeFilter === type ? " mem-filter__item--on" : "")}
								onClick={() => setTypeFilter(type)}
								type="button"
								key={type}
							>
								{memoryTypeLabel(type, t)}
							</button>
						))}
					</div>
				</div>}
				{archives.length === 0 ? (
					<div className="mem-empty mem-empty--cta">
						<strong>{t("memory.emptyArchivedTitle")}</strong>
						<span>{t("memory.emptyArchivedBody")}</span>
					</div>
				) : (
					<ArchivedMemoryList
						archives={filteredArchives}
						totalArchives={archives.length}
						expanded={expandedArchive}
						setExpanded={setExpandedArchive}
						renderWithLinks={renderWithLinks}
						t={t}
						hideHeader
						busy={busy}
						onRestore={restoreArchive}
					/>
				)}
			</section>}

			{tab === "docs" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.instructionFiles")}</div>
						<div className="mem-note">{t("memory.instructionFilesHint")}</div>
					</div>
					<div className="mem-section__actions">
						<button
							className="btn btn--small"
							type="button"
							disabled={busy}
							onClick={() => setShowAdd((v) => !v)}
						>
							{showAdd ? t("common.collapse") : <><Plus size={13} />{t("memory.addMemory")}</>}
						</button>
					</div>
				</div>
				{view.instructionDiagnostics.length > 0 && (
					<div className="mem-instruction-diagnostics">
						<div className="mem-instruction-diagnostics__title"><AlertTriangle size={14} />{t("memory.instructionDiagnostics")}</div>
						{view.instructionDiagnostics.map((diagnostic, index) => (
							<div className="mem-instruction-diagnostic" key={`${diagnostic.code}:${diagnostic.path}:${diagnostic.line || index}`}>
								<strong>{diagnostic.code}</strong>
								<span>{diagnostic.message}</span>
								<code>{diagnostic.path}{diagnostic.line ? `:${diagnostic.line}` : ""}</code>
							</div>
						))}
					</div>
				)}
				{showAdd && (
					<div className="mem-add-card">
						<div className="mem-add-card__head">
							<div>
								<strong>{t("memory.addMemory")}</strong>
								<span>{t("memory.addMemoryHint")}</span>
							</div>
						</div>
						<div className="mem-add">
							<Tooltip label={t("memory.whereToSave")}>
								<select
									className="mem-select"
									value={activeScope}
									onChange={(e) => setScope(e.target.value)}
								>
									{scopes.map((s) => (
										<option key={s.scope} value={s.scope}>
											{memoryScopeLabel(s.scope, t)}
										</option>
									))}
								</select>
							</Tooltip>
							<input
								className="mem-input"
								placeholder={t("memory.notePlaceholder")}
								value={note}
								onChange={(e) => setNote(e.target.value)}
								onKeyDown={(e) => {
									if (e.key === "Enter") void submitNote();
								}}
							/>
							<button
								className="btn btn--primary btn--small"
								onClick={() => void submitNote()}
								disabled={busy || !note.trim()}
							>
								{t("memory.remember")}
							</button>
						</div>
						<div className="mem-hint">
							{scopes.find((s) => s.scope === activeScope)?.path}
						</div>
					</div>
				)}
				{view.docs.length === 0 && (
					<div className="mem-empty">{t("memory.noDocs")}</div>
				)}
				{view.docs.map((d) => {
					const editing = editingPath === d.path;
					const open = expandedDoc === d.path || editing;
					return (
						<div className="mem-doc" data-doc-scope={d.scope || "other"} key={d.path}>
							<div className="mem-doc__head">
								<button
									className="mem-doc__identity mem-doc__toggle"
									type="button"
									aria-expanded={open}
									onClick={() => {
										if (!editing) setExpandedDoc(open ? null : d.path);
									}}
									disabled={editing}
								>
									<span className="mem-doc__chevron">
										{open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
									</span>
									<span className="mem-doc__icon"><FileText size={15} /></span>
									<div>
										<strong>{memoryDocTitle(d.scope, t)}</strong>
										<span className="mem-doc__path">{d.path}</span>
										<small>{memoryDocHint(d.scope, t)}</small>
										<small>{t("memory.instructionPrecedence", { precedence: d.precedence + 1, directory: d.directory || t("memory.globalDirectory") })}</small>
									</div>
								</button>
								<div className="mem-doc__head-actions">
									<span className={"mem-doc__tag badge--" + d.scope}>{memoryScopeLabel(d.scope, t)}</span>
									{!editing && (
									<button
										className="btn btn--small"
										onClick={() => startEdit(d.path, d.body)}
									>
										<Pencil size={13} />
										{t("common.edit")}
									</button>
									)}
								</div>
							</div>
							{editing ? (
								<div className="mem-doc__edit">
									<textarea
										className="mem-textarea"
										value={draft}
										onChange={(e) => setDraft(e.target.value)}
										spellCheck={false}
									/>
									<div className="mem-doc__actions">
										<button
											className="btn btn--small"
											onClick={() => setEditingPath(null)}
											disabled={busy}
										>
											{t("common.cancel")}
										</button>
										<button
											className="btn btn--primary btn--small"
											onClick={() => void saveEdit()}
											disabled={busy}
										>
											{t("common.save")}
										</button>
									</div>
								</div>
							) : open ? (
								<div className="mem-doc__expanded">
									<pre className="mem-doc__body">{d.body}</pre>
									{d.imports.length > 0 && (
										<div className="mem-doc__imports">
											<strong>{t("memory.instructionImports")}</strong>
											{d.imports.map((item) => <code key={`${item.sourcePath}:${item.path}`}>{item.sourcePath} → {item.path}</code>)}
										</div>
									)}
								</div>
							) : null}
						</div>
					);
				})}
			</section>}
		</>
	);
}
