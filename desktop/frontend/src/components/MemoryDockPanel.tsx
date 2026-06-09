import { useCallback, useEffect, useMemo, useState } from "react";
import { Search, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT, type DictKey } from "../lib/i18n";
import type { MemoryFact, MemoryView } from "../lib/types";

function displayTitle(fact: MemoryFact): string {
  return fact.title || fact.name.replace(/[-_]/g, " ");
}

function MemoryDockPanel() {
  const t = useT();
  const [view, setView] = useState<MemoryView | null>(null);
  const [search, setSearch] = useState("");
  const [expandedFact, setExpandedFact] = useState<string | null>(null);
  const [typeFilter, setTypeFilter] = useState("all");

  const reload = useCallback(async () => {
    setView(await app.Memory().catch(() => null));
  }, []);
  useEffect(() => { void reload(); }, [reload]);

  const facts = view?.facts ?? [];
  const globalFacts = view?.globalFacts ?? [];

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

  return (
    <div className="workspace-files">
      <div className="workspace-files__tools">
        <div className="workspace-search" style={{ flex: 1, margin: "4px 0 8px" }}>
          <Search size={14} />
          <input
            placeholder={t("memory.searchPlaceholder")}
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
        <div className="mem-filter" style={{ margin: "0 8px 6px" }}>
          <button
            className={`mem-filter__item${typeFilter === "all" ? " mem-filter__item--on" : ""}`}
            onClick={() => setTypeFilter("all")}
          >
            {t("memory.allTypes")}
          </button>
          {factTypes.map((ft) => (
            <button
              key={ft}
              className={`mem-filter__item${typeFilter === ft ? " mem-filter__item--on" : ""}`}
              onClick={() => setTypeFilter(ft)}
            >
              {t(("memory.typeLabel." + ft) as DictKey) || ft}
            </button>
          ))}
        </div>
      )}

      <div className="mem-facts" style={{ flex: 1, overflowY: "auto", padding: "0 8px 8px" }}>
        {!view ? (
          <div className="workspace-empty">{t("workspace.loading")}</div>
        ) : (
          <div>
            {/* Project-level facts */}
            <div className="mem-section__title" style={{ padding: "8px 0 4px", margin: 0 }}>
              {t("memory.savedMemories")} ({filteredFacts.length})
            </div>
            {filteredFacts.length === 0 ? (
              <div className="workspace-empty" style={{ marginBottom: 16 }}>{t("memory.noFacts")}</div>
            ) : (
              filteredFacts.map((f) => {
                const isExpanded = expandedFact === f.name;
                return (
                  <div
                    key={f.name}
                    className="mem-fact"
                    style={{ marginBottom: 4 }}
                  >
                    <button
                      className="mem-fact__summary"
                      onClick={() => setExpandedFact(isExpanded ? null : f.name)}
                    >
                      <span className="mem-fact__main">
                        <span className={`mem-fact__meta mem-fact__meta--${f.type}`}>{f.type}</span>
                        <span className="mem-fact__title">{displayTitle(f)}</span>
                      </span>
                    </button>
                    {isExpanded && f.body && (
                      <div className="mem-fact__detail">
                        <div className="mem-fact__body">{f.body}</div>
                      </div>
                    )}
                  </div>
                );
              })
            )}

            {/* Global facts */}
            {globalFacts.length > 0 && (
              <>
                <div
                  className="mem-section__title"
                  style={{ padding: "12px 0 4px", margin: 0, borderTop: "1px solid var(--border-soft)" }}
                >
                  {t("memory.globalMemories")} ({globalFacts.length})
                </div>
                {globalFacts.map((f) => {
                  const isExpanded = expandedFact === f.name;
                  return (
                    <div
                      key={f.name}
                      className="mem-fact"
                      style={{ marginBottom: 4 }}
                    >
                      <button
                        className="mem-fact__summary"
                        onClick={() => setExpandedFact(isExpanded ? null : f.name)}
                      >
                        <span className="mem-fact__main">
                          <span className={`mem-fact__meta mem-fact__meta--${f.type}`}>{f.type}</span>
                          <span className="mem-fact__title">{displayTitle(f)}</span>
                        </span>
                      </button>
                      {isExpanded && f.body && (
                        <div className="mem-fact__detail">
                          <div className="mem-fact__body">{f.body}</div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export { MemoryDockPanel };
