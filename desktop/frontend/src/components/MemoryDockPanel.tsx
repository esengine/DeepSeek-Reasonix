import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Book, BrainCircuit, ChevronDown, ChevronRight, FileText, Search, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { MemoryFact, MemoryView } from "../lib/types";

function displayTitle(fact: MemoryFact): string {
  return fact.title || fact.name.replace(/[-_]/g, " ");
}

const TYPE_LABEL: Record<string, string> = {
  user: "User",
  feedback: "Feedback",
  project: "Project",
  reference: "Reference",
};

function FactIcon({ type }: { type: string }) {
  return (
    <span className={`mem-fact__dot mem-fact__dot--${type || "project"}`} title={TYPE_LABEL[type] || type} />
  );
}

function MemoryDockPanel() {
  const t = useT();
  const [view, setView] = useState<MemoryView | null>(null);
  const [search, setSearch] = useState("");
  const [expandedFact, setExpandedFact] = useState<string | null>(null);
  const [expandedDoc, setExpandedDoc] = useState<string | null>(null);
  const [typeFilter, setTypeFilter] = useState("all");
  const factRefs = useRef<Record<string, HTMLElement | null>>({});

  const reload = useCallback(async () => {
    setView(await app.Memory().catch(() => null));
  }, []);
  useEffect(() => { void reload(); }, [reload]);

  const facts = view?.facts ?? [];
  const docs = view?.docs ?? [];

  const factTypes = useMemo(
    () => Array.from(new Set(facts.map((f) => f.type).filter(Boolean))).sort(),
    [facts],
  );

  const q = search.trim().toLowerCase();

  const filteredFacts = useMemo(
    () =>
      facts.filter((f) => {
        if (typeFilter !== "all" && f.type !== typeFilter) return false;
        if (!q) return true;
        return [displayTitle(f), f.name, f.description, f.body].join(" ").toLowerCase().includes(q);
      }),
    [facts, q, typeFilter],
  );

  const filteredDocs = useMemo(
    () =>
      docs.filter((d) => {
        if (!q) return true;
        return [d.path, d.body].join(" ").toLowerCase().includes(q);
      }),
    [docs, q],
  );

  return (
    <div className="workspace-files">
      <div className="workspace-files__tools">
        <div className="workspace-search" style={{ flex: 1 }}>
          <Search size={14} />
          <input
            placeholder="Filter memories…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {search && (
            <button className="workspace-iconbtn" onClick={() => setSearch("")}>
              <X size={12} />
            </button>
          )}
        </div>
      </div>

      {factTypes.length > 1 && (
        <div className="mem-filter" style={{ margin: "0 10px 6px" }}>
          <button
            className={`mem-filter__item${typeFilter === "all" ? " mem-filter__item--on" : ""}`}
            onClick={() => setTypeFilter("all")}
          >
            All
          </button>
          {factTypes.map((ft) => (
            <button
              key={ft}
              className={`mem-filter__item${typeFilter === ft ? " mem-filter__item--on" : ""}`}
              onClick={() => setTypeFilter(ft)}
            >
              {TYPE_LABEL[ft] || ft}
            </button>
          ))}
        </div>
      )}

      <div className="mem-facts" style={{ flex: 1, overflowY: "auto", padding: "0 8px 8px" }}>
        {!view ? (
          <div className="workspace-empty">{t("workspace.loading")}</div>
        ) : filteredFacts.length === 0 && filteredDocs.length === 0 ? (
          <div className="workspace-empty">No memories</div>
        ) : (
          <>
            {filteredFacts.length > 0 && (
              <div>
                <div className="mem-section__title" style={{ padding: "8px 2px 4px", margin: 0 }}>
                  <BrainCircuit size={12} style={{ marginRight: 4, verticalAlign: "middle" }} />
                  Facts ({filteredFacts.length})
                </div>
                {filteredFacts.map((f) => {
                  const isExpanded = expandedFact === f.name;
                  return (
                    <div
                      key={f.name}
                      ref={(el) => { factRefs.current[f.name] = el; }}
                      className="mem-fact"
                      style={{ marginBottom: 4 }}
                    >
                      <button
                        className="mem-fact__summary"
                        onClick={() => setExpandedFact(isExpanded ? null : f.name)}
                      >
                        <FactIcon type={f.type} />
                        <span className="mem-fact__main">
                          <span className="mem-fact__title">{displayTitle(f)}</span>
                          {f.description && (
                            <span className="mem-fact__desc">{f.description}</span>
                          )}
                          <span className="mem-fact__meta">{f.type}</span>
                        </span>
                        {isExpanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                      </button>
                      {isExpanded && f.body && (
                        <div className="mem-fact__detail">
                          <div className="mem-fact__body">{f.body}</div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}

            {filteredDocs.length > 0 && (
              <div>
                <div className="mem-section__title" style={{ padding: "12px 2px 4px", margin: 0 }}>
                  <FileText size={12} style={{ marginRight: 4, verticalAlign: "middle" }} />
                  Docs ({filteredDocs.length})
                </div>
                {filteredDocs.map((d) => {
                  const isExpanded = expandedDoc === d.path;
                  return (
                    <div key={d.path} className="mem-doc" style={{ marginBottom: 6 }}>
                      <button
                        className="mem-doc__head"
                        onClick={() => setExpandedDoc(isExpanded ? null : d.path)}
                        style={{ cursor: "pointer", width: "100%", border: "none", textAlign: "left", font: "inherit", color: "inherit", background: "var(--bg-soft)" }}
                      >
                        <span className="mem-doc__identity">
                          <Book size={14} style={{ flex: "0 0 auto", marginTop: 1 }} />
                          <div>
                            <strong>{d.path}</strong>
                            <small>{d.scope}</small>
                          </div>
                        </span>
                        {isExpanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                      </button>
                      {isExpanded && (
                        <div className="mem-doc__body">
                          {d.body || "(empty)"}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export { MemoryDockPanel };
