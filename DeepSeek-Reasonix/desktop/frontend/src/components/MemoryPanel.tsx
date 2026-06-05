import { ChevronDown, ChevronRight, Search, Trash2 } from "lucide-react";
import { useMemo, useRef, useState, type ReactNode } from "react";
import { useT } from "../lib/i18n";
import type { MemoryFact, MemoryView } from "../lib/types";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";

type LinkInfo = {
  name: string;
  exists: boolean;
};

function displayTitle(fact: MemoryFact): string {
  return fact.title || fact.name.replaceAll("-", " ");
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
  const [confirmForget, setConfirmForget] = useState<string | null>(null);
  const factRefs = useRef<Record<string, HTMLElement | null>>({});

  const facts = view?.facts ?? [];
  const factNames = useMemo(() => new Set(facts.map((f) => f.name)), [facts]);
  const factTypes = useMemo(
    () => Array.from(new Set(facts.map((f) => f.type).filter(Boolean))).sort(),
    [facts],
  );
  const normalizedQuery = query.trim().toLowerCase();
  const filteredFacts = useMemo(
    () =>
      facts.filter((f) => {
        if (typeFilter !== "all" && f.type !== typeFilter) return false;
        if (!normalizedQuery) return true;
        return [displayTitle(f), f.name, f.description, f.type, f.body]
          .join(" ")
          .toLowerCase()
          .includes(normalizedQuery);
      }),
    [facts, normalizedQuery, typeFilter],
  );

  const scrollToFact = (name: string) => {
    const el = factRefs.current[name];
    if (!el) return;
    el.scrollIntoView({ block: "center", behavior: "smooth" });
    setHighlight(name);
    window.setTimeout(() => setHighlight((h) => (h === name ? null : h)), 1200);
  };

  // Clear active filters when the target is hidden, else the [[link]] is a silent no-op.
  const jumpTo = (name: string) => {
    if (!factNames.has(name)) return;
    const visible = filteredFacts.some((f) => f.name === name);
    setExpanded(name);
    setConfirmForget(null);
    if (!visible) {
      setQuery("");
      setTypeFilter("all");
      window.setTimeout(() => scrollToFact(name), 0);
      return;
    }
    scrollToFact(name);
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

  const forgetFact = async (name: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await onForget(name);
      if (expanded === name) setExpanded(null);
      setConfirmForget(null);
    } finally {
      setBusy(false);
    }
  };

  const scopes = view?.scopes ?? [];
  // Default the scope selector to "project" when present, else the first option.
  const activeScope =
    scope || scopes.find((s) => s.scope === "project")?.scope || scopes[0]?.scope || "project";

  const submitNote = async () => {
    const trimmed = note.trim();
    if (!trimmed || busy) return;
    setBusy(true);
    try {
      await onRemember(activeScope, trimmed);
      setNote("");
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
    try {
      await onSaveDoc(editingPath, draft);
      setEditingPath(null);
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
                {t("memory.summary", { facts: facts.length, docs: view.docs.length })}
              </div>
            )}
          </div>
          <Tooltip label={t("common.close")}>
            <button className="chip" onClick={onClose}>
              ✕
            </button>
          </Tooltip>
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
                      {type}
                    </button>
                  ))}
                </div>
              </div>
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
                    const isOpen = expanded === f.name;
                    const links = uniqueLinks(f.body, factNames);
                    const missing = links.filter((link) => !link.exists);
                    return (
                      <article
                        className={`mem-fact${highlight === f.name ? " mem-fact--hl" : ""}`}
                        key={f.name}
                        ref={(el) => {
                          factRefs.current[f.name] = el;
                        }}
                      >
                        <button
                          className="mem-fact__summary"
                          onClick={() => {
                            setExpanded(isOpen ? null : f.name);
                            setConfirmForget(null);
                          }}
                          type="button"
                        >
                          {isOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
                          <span className="mem-fact__main">
                            <span className="mem-fact__title">{displayTitle(f)}</span>
                            <span className="mem-fact__meta">
                              {f.name} · {f.type}
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
                              {confirmForget === f.name ? (
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
                                    onClick={() => void forgetFact(f.name)}
                                    disabled={busy}
                                    type="button"
                                  >
                                    {t("memory.confirmForget")}
                                  </button>
                                </div>
                              ) : (
                                <button
                                  className="btn btn--small mem-fact__forget"
                                  onClick={() => setConfirmForget(f.name)}
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
              {view.storeDir && (
                <div className="mem-hint">{t("memory.storedUnder", { dir: view.storeDir })}</div>
              )}
            </section>

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
              {view.docs.length === 0 && (
                <div className="mem-empty">{t("memory.noDocs")}</div>
              )}
              {view.docs.map((d) => {
                const editing = editingPath === d.path;
                return (
                  <div className="mem-doc" key={d.path}>
                    <div className="mem-doc__head">
                      <span className={`badge badge--${d.scope}`}>{d.scope}</span>
                      <span className="mem-doc__path">{d.path}</span>
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
          </div>
        )}
    </ResizableDrawer>
  );
}
