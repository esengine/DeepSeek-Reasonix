import { ChevronDown, ChevronRight, FileText, Search, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { MemoryFact, MemoryView } from "../lib/types";
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

function memoryDocPreview(body: string): string {
  const lines = body.split(/\r?\n/);
  const preview = lines.slice(0, 6).join("\n");
  return lines.length > 6 ? `${preview}\n...` : preview;
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err || "Unknown error");
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
  const [error, setError] = useState<string | null>(null);
  const factRefs = useRef<Record<string, HTMLElement | null>>({});

  // Filter input — a single substring search across docs and facts. The
  // substring is case-insensitive and matches anywhere in the body or the
  // path; an empty string shows everything. The filter is purely frontend
  // (no kernel round-trip) so it's instant and reversible.
  const [filter, setFilter] = useState("");

  const facts = view?.facts ?? [];
  const factNames = useMemo(() => new Set(facts.map((f) => f.name)), [facts]);
  const factTypes = useMemo(
    () => Array.from(new Set(facts.map((f) => f.type).filter(Boolean))).sort(),
    [facts],
  );
  const normalizedQuery = query.trim().toLowerCase();
  const normalizedFilter = filter.trim().toLowerCase();
  const filteredFacts = useMemo(
    () =>
      facts.filter((f) => {
        if (typeFilter !== "all" && f.type !== typeFilter) return false;
        if (normalizedFilter) {
          const hay = [f.name, f.description, f.body].join(" ").toLowerCase();
          if (!hay.includes(normalizedFilter)) return false;
        }
        if (!normalizedQuery) return true;
        return [displayTitle(f), f.name, f.description, f.type, f.body]
          .join(" ")
          .toLowerCase()
          .includes(normalizedQuery);
      }),
    [facts, normalizedQuery, normalizedFilter, typeFilter],
  );

  const scrollToFact = (name: string) => {
    const el = factRefs.current[name];
    if (!el) return;
    el.scrollIntoView({ block: "center", behavior: "auto" });
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
    setError(null);
    try {
      await onForget(name);
      if (expanded === name) setExpanded(null);
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
                {t("memory.summary", { facts: facts.length, docs: view.docs.length })}
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
                      {type}
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
                    const isOpen = expanded === f.name;
                    const links = uniqueLinks(f.body, factNames);
                    const missing = links.filter((link) => !link.exists);
                    return (
                      <article
                        className={`mem-fact${highlight === f.name ? " mem-fact--hl" : ""}`}
                        data-mem-type={f.type || "other"}
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
                              {f.type && <span className="mem-fact__type" data-mem-type={f.type}>{f.type}</span>}
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
                  <div className="mem-doc" key={d.path}>
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
                  <div className="mem-fact" key={f.name} title={f.body}>
                    <span className={`badge badge--${f.type}`}>{f.type}</span>
                    <div className="mem-fact__text">
                      <div className="mem-fact__name">{f.name}</div>
                      <div className="mem-fact__desc">{f.description}</div>
                    </div>
                  </div>
                ))
              )}
              {view.storeDir && (
                <div className="mem-hint" title={view.storeDir}>
                  {t("memory.storedUnder", { dir: view.storeDir })}
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
	const [note, setNote] = useState("");
	const [scope, setScope] = useState("");
	const [editingPath, setEditingPath] = useState<string | null>(null);
	const [draft, setDraft] = useState("");
	const [busy, setBusy] = useState(false);
	const [highlight, setHighlight] = useState<string | null>(null);
	const [query, setQuery] = useState("");
	const [typeFilter, setTypeFilter] = useState("all");
	const [expanded, setExpanded] = useState<string | null>(null);
	const [expandedDoc, setExpandedDoc] = useState<string | null>(null);
	const [confirmForget, setConfirmForget] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [tab, setTab] = useState<"memories" | "docs">("memories");
	const [showAdd, setShowAdd] = useState(false);
	const factRefs = useRef<Record<string, HTMLElement | null>>({});

	const reload = useCallback(async () => {
		setView(await app.Memory().catch(() => null));
	}, []);
	useEffect(() => { void reload(); }, [reload]);

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

	const scrollToFact = useCallback((name: string) => {
		const el = factRefs.current[name];
		if (!el) return;
		el.scrollIntoView({ block: "center", behavior: "auto" });
		setHighlight(name);
		window.setTimeout(() => setHighlight((h) => (h === name ? null : h)), 1200);
	}, []);

	const jumpTo = useCallback((name: string) => {
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
	}, [factNames, filteredFacts, scrollToFact]);

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

	const forgetFact = useCallback(async (name: string) => {
		if (busy) return;
		setBusy(true);
		setError(null);
		try {
			await app.Forget(name);
			await reload();
			if (expanded === name) setExpanded(null);
			setConfirmForget(null);
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [busy, expanded, reload]);

	const scopes = view?.scopes ?? [];
	const activeScope =
		scope || scopes.find((s) => s.scope === "project")?.scope || scopes[0]?.scope || "project";

	const submitNote = useCallback(async () => {
		const trimmed = note.trim();
		if (!trimmed || busy) return;
		setBusy(true);
		setError(null);
		try {
			await app.Remember(activeScope, trimmed);
			await reload();
			setNote("");
			setShowAdd(false);
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [note, busy, activeScope, reload]);

	const startEdit = useCallback((path: string, body: string) => {
		setEditingPath(path);
		setDraft(body);
	}, []);

	const saveEdit = useCallback(async () => {
		if (editingPath === null || busy) return;
		setBusy(true);
		setError(null);
		try {
			await app.SaveDoc(editingPath, draft);
			await reload();
			setEditingPath(null);
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}, [editingPath, busy, draft, reload]);

	if (!view?.available) {
		return <div className="empty">{t("memory.unavailable")}</div>;
	}

	return (
		<>
			<div className="settings-subtabs" role="tablist" aria-label={t("settings.tab.memory")}>
				<button
					className={"settings-subtab" + (tab === "memories" ? " settings-subtab--active" : "")}
					role="tab"
					aria-selected={tab === "memories"}
					type="button"
					onClick={() => setTab("memories")}
				>
					<span>{t("memory.memoryEntries")}</span>
					<small>{facts.length}</small>
				</button>
				<button
					className={"settings-subtab" + (tab === "docs" ? " settings-subtab--active" : "")}
					role="tab"
					aria-selected={tab === "docs"}
					type="button"
					onClick={() => setTab("docs")}
				>
					<span>{t("memory.instructionFiles")}</span>
					<small>{view.docs.length}</small>
				</button>
			</div>

			{tab === "memories" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.memoryEntries")}</div>
						<div className="mem-note">{t("memory.fallibleNote")}</div>
					</div>
					<div className="mem-section__actions">
						<span className="mem-count">{facts.length}</span>
						<button
							className="btn btn--small"
							type="button"
							disabled={busy}
							onClick={() => setShowAdd((v) => !v)}
						>
							{showAdd ? t("common.collapse") : t("memory.addMemory")}
						</button>
					</div>
				</div>
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
								{type}
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
							const isOpen = expanded === f.name;
							const links = uniqueLinks(f.body, factNames);
							const missing = links.filter((link) => !link.exists);
							return (
								<article
									className={"mem-fact" + (highlight === f.name ? " mem-fact--hl" : "")}
									data-mem-type={f.type || "other"}
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
												{f.type && <span className="mem-fact__type" data-mem-type={f.type}>{f.type}</span>}
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
			</section>}

			{tab === "docs" && <section className="mem-section">
				<div className="mem-section__head">
					<div>
						<div className="mem-section__title">{t("memory.instructionFiles")}</div>
						<div className="mem-note">{t("memory.instructionFilesHint")}</div>
					</div>
					<span className="mem-count">{view.docs.length}</span>
				</div>
				{view.docs.length === 0 && (
					<div className="mem-empty">{t("memory.noDocs")}</div>
				)}
				{view.docs.map((d) => {
					const editing = editingPath === d.path;
					const open = expandedDoc === d.path || editing;
					return (
						<div className="mem-doc" key={d.path}>
							<div className="mem-doc__head">
								<div className="mem-doc__identity">
									<span className="mem-doc__icon"><FileText size={15} /></span>
									<div>
										<strong>{memoryDocTitle(d.scope, t)}</strong>
										<span className="mem-doc__path">{d.path}</span>
										<small>{memoryDocHint(d.scope, t)}</small>
									</div>
								</div>
								<div className="mem-doc__head-actions">
									<span className={"mem-doc__tag badge--" + d.scope}>{memoryScopeLabel(d.scope, t)}</span>
									{!editing && (
										<button
											className="btn btn--small"
											type="button"
											onClick={() => setExpandedDoc(open ? null : d.path)}
										>
											{open ? t("common.collapse") : t("memory.expandDoc")}
										</button>
									)}
									{!editing && (
									<button
										className="btn btn--small"
										onClick={() => startEdit(d.path, d.body)}
									>
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
							) : (
								<pre className={"mem-doc__body" + (!open ? " mem-doc__body--preview" : "")}>
									{open ? d.body : memoryDocPreview(d.body)}
								</pre>
							)}
						</div>
					);
				})}
			</section>}
		</>
	);
}
