import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Brain, Check, ChevronsUpDown, Search } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ModelInfo } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";
import { Tooltip } from "./Tooltip";

// ModelSwitcher opens an upward popover listing configured providers. Selecting
// one switches the active model while the current conversation continues.
export function ModelSwitcher({
  label,
  tabId,
  onPick,
}: {
  label: string;
  tabId?: string;
  onPick: (name: string) => boolean | Promise<boolean>;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [query, setQuery] = useState("");
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>(undefined);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const loadSeqRef = useRef(0);
  const currentTabKeyRef = useRef(tabId ?? "");
  const pendingPickCountByTabRef = useRef(new Map<string, number>());
  const pickSeqByTabRef = useRef(new Map<string, number>());
  currentTabKeyRef.current = tabId ?? "";

  // Measure trigger width off the render path to avoid forced layout
  useEffect(() => {
    const el = triggerRef.current;
    if (!el) return;
    const measure = () => setTriggerWidth(el.getBoundingClientRect().width);
    measure();
    const observer = new ResizeObserver(() => measure());
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const loadModelsForTab = useCallback((targetTabId?: string) => {
    const targetKey = targetTabId ?? "";
    const seq = ++loadSeqRef.current;
    return (targetTabId ? app.ModelsForTab(targetTabId) : app.Models())
      .then((next) => {
        if (seq === loadSeqRef.current && currentTabKeyRef.current === targetKey) {
          setModels(asArray(next));
        }
      })
      .catch(() => {});
  }, []);

  const loadModels = useCallback(
    () => loadModelsForTab(tabId),
    [loadModelsForTab, tabId],
  );

  useEffect(() => {
    void loadModels();
  }, [loadModels]);

  useEffect(() => {
    const refresh = () => void loadModels();
    window.addEventListener("reasonix:model-catalog-changed", refresh);
    return () => window.removeEventListener("reasonix:model-catalog-changed", refresh);
  }, [loadModels]);

  useEffect(() => {
    if (open) {
      setQuery("");
      void loadModels();
      window.requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [loadModels, open]);

  const keyword = query.trim().toLowerCase();
  const filtered = useMemo(
    () => keyword
      ? models.filter((m) => m.model.toLowerCase().includes(keyword) || m.provider.toLowerCase().includes(keyword))
      : models,
    [models, keyword],
  );

  // Group by provider, with the current model's group first
  const groups = useMemo(() => {
    const map = new Map<string, ModelInfo[]>();
    let currentProvider = "";
    for (const m of filtered) {
      if (m.current) currentProvider = m.provider;
      const list = map.get(m.provider);
      if (list) list.push(m);
      else map.set(m.provider, [m]);
    }
    return [...map.entries()]
      .sort(([a], [b]) => {
        if (a === currentProvider) return -1;
        if (b === currentProvider) return 1;
        return providerLabel(a, t).localeCompare(providerLabel(b, t));
      })
      .map(([provider, items]) => ({
        provider,
        label: providerLabel(provider, t),
        items,
      }));
  }, [filtered, t]);

  const currentProvider = useMemo(() => {
    const cur = models.find((m) => m.current) ?? models.find((m) => m.model === label || m.ref === label);
    return cur ? providerLabel(cur.provider, t) : null;
  }, [label, models, t]);
  const triggerLabel = currentProvider ? `${label} · ${currentProvider}` : label;

  const pick = (model: ModelInfo) => {
    setOpen(false);
    const pendingKey = tabId ?? "";
    const pendingPickCount = pendingPickCountByTabRef.current.get(pendingKey) ?? 0;
    // A catalog refresh can still report the outgoing model as current while
    // an earlier switch is rebuilding. In that window, selecting it again is
    // an intentional last-click-wins rollback rather than a no-op.
    if (model.current && pendingPickCount === 0) return;
    const previousModels = models;
    const pickSeq = (pickSeqByTabRef.current.get(pendingKey) ?? 0) + 1;
    pickSeqByTabRef.current.set(pendingKey, pickSeq);
    // Catalog requests started before this click describe the outgoing model
    // and must not overwrite the optimistic last-click choice.
    loadSeqRef.current += 1;
    setModels((prev) => prev.map((m) => ({ ...m, current: m.ref === model.ref })));
    pendingPickCountByTabRef.current.set(pendingKey, pendingPickCount + 1);
    const settlePick = (switched: boolean) => {
      const nextCount = Math.max(
        0,
        (pendingPickCountByTabRef.current.get(pendingKey) ?? 0) - 1,
      );
      if (nextCount === 0) pendingPickCountByTabRef.current.delete(pendingKey);
      else pendingPickCountByTabRef.current.set(pendingKey, nextCount);
      // A superseded completion no longer owns the visible selection. Only the
      // latest failed click may roll back and reconcile with the backend.
      if (
        switched ||
        pickSeqByTabRef.current.get(pendingKey) !== pickSeq ||
        currentTabKeyRef.current !== pendingKey
      ) {
        return;
      }
      setModels(previousModels);
      void loadModelsForTab(tabId);
    };
    try {
      void Promise.resolve(onPick(model.ref)).then(
        (switched) => settlePick(switched),
        () => settlePick(false),
      );
    } catch (err) {
      settlePick(false);
      throw err;
    }
  };

  return (
    <div className="modelsw">
      <Tooltip label={triggerLabel} fill>
        <button
          ref={triggerRef}
          type="button"
          className="modelsw__trigger"
          aria-label={triggerLabel}
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          <Brain size={14} className="modelsw__kind" />
          <span className="modelsw__label">{label}</span>
          <ChevronsUpDown size={11} />
        </button>
      </Tooltip>
      <AnchoredPopover
        open={open}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="modelsw__menu modelsw__menu--portal"
        style={{ minWidth: Math.max(triggerWidth || 200, 200), maxWidth: "min(90vw, 480px)" }}
      >
        <div role="listbox">
          <div className="modelsw__search" role="presentation">
            <Search size={13} />
            <input
              ref={inputRef}
              type="text"
              className="modelsw__search-input"
              placeholder={t("modelSwitcher.searchPlaceholder")}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") setOpen(false);
                if (e.key === "Enter" && filtered.length === 1) pick(filtered[0]);
              }}
            />
          </div>
          {models.length === 0 && <div className="modelsw__empty">{t("status.noModels")}</div>}
          {models.length > 0 && filtered.length === 0 && query && <div className="modelsw__empty">{t("modelSwitcher.noMatches")}</div>}
          {groups.map((g) => (
            <div key={g.provider} role="group" aria-label={g.label} className="modelsw__group">
              <div className="modelsw__group-label" role="presentation"><Brain size={11} />{g.label}</div>
              {g.items.map((m) => (
                <button
                  key={m.ref}
                  type="button"
                  role="option"
                  aria-selected={m.current}
                  className={`modelsw__item ${m.current ? "modelsw__item--current" : ""}`}
                  onClick={() => pick(m)}
                >
                  <span className="modelsw__copy">
                    <span className="modelsw__model">{m.model}</span>
                  </span>
                  {m.current && <Check size={13} className="modelsw__check" />}
                </button>
              ))}
            </div>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function providerLabel(provider: string, t: ReturnType<typeof useT>): string {
  switch (provider) {
    case "deepseek":
    case "deepseek-flash":
    case "deepseek-pro":
      return t("settings.providerLabel.deepseek");
    default:
      return provider;
  }
}
