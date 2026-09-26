import { formatTokens } from "../lib/format";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Check, ChevronDown, ChevronRight, Cpu, Image, List, Plus, Search, Star } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { readModelFavorites, writeModelFavorites } from "../lib/modelFavorites";
import { orderByProvider, useProviderOrder } from "../lib/providerOrder";
import { providerBrandIcons } from "../lib/providerBrandIcons";
import type { ModelInfo } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";
import { Tooltip } from "./Tooltip";
import { useModelApplicationStatus } from "../lib/useModelApplicationStatus";

// ModelSwitcher opens an upward popover listing configured providers. Selecting
// one switches the active model while the current conversation continues.
export function ModelSwitcher({
  label,
  tabId,
  draftId,
  ready = true,
  sessionKey,
  onPick,
  onManage,
  detailLabel,
  details,
  composerMenu = false,
  disabled = false,
  dismissSignal,
}: {
  label: string;
  detailLabel?: string;
  details?: ReactNode;
  composerMenu?: boolean;
  disabled?: boolean;
  dismissSignal?: number;
  tabId?: string;
  draftId?: string;
  ready?: boolean;
  sessionKey?: string;
  onPick: (name: string) => boolean | Promise<boolean>;
  onManage?: () => void;
}) {
  const t = useT();
  const application=useModelApplicationStatus(tabId);
  const [open, setOpen] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [query, setQuery] = useState("");
  const [activeFilter, setActiveFilter] = useState("all");
  const [favorites, setFavorites] = useState<Set<string>>(() => readModelFavorites());
  const providerOrder = useProviderOrder();
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>(undefined);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const loadSeqRef = useRef(0);
  const currentTabKeyRef = useRef(draftId ? `draft:${draftId}` : tabId ?? "");
  const pendingPickCountByTabRef = useRef(new Map<string, number>());
  const pickSeqByTabRef = useRef(new Map<string, number>());
  currentTabKeyRef.current = draftId ? `draft:${draftId}` : tabId ?? "";

  useEffect(() => {
    setOpen(false);
  }, [disabled, dismissSignal, draftId, sessionKey, tabId]);

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

  const loadModelsForTab = useCallback((targetTabId?: string, targetDraftId?: string) => {
    const targetKey = targetDraftId ? `draft:${targetDraftId}` : targetTabId ?? "";
    const seq = ++loadSeqRef.current;
    return (targetDraftId ? app.ModelsForDraft(targetDraftId) : targetTabId ? app.ModelsForTab(targetTabId) : app.Models())
      .then((next) => {
        if (seq === loadSeqRef.current && currentTabKeyRef.current === targetKey) {
          setModels(asArray(next).map(normalizeModelInfo));
        }
      })
      .catch(() => {});
  }, []);

  const loadModels = useCallback(
    () => loadModelsForTab(tabId, draftId),
    [draftId, loadModelsForTab, tabId],
  );

  useEffect(() => {
    void loadModels();
  }, [loadModels, ready, sessionKey, label]);

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

  const orderedModels = useMemo(() => orderByProvider(models, (model) => model.provider, providerOrder), [models, providerOrder]);
  const providers = useMemo(() => {
    const seen = new Set<string>();
    return orderedModels.flatMap((model) => {
      if (seen.has(model.provider)) return [];
      seen.add(model.provider);
      return [{
        id: model.provider,
        label: model.displayName?.trim() || providerLabel(model.provider, t),
      }];
    });
  }, [orderedModels, t]);

  useEffect(() => {
    if (activeFilter !== "all" && activeFilter !== "favorites" && !providers.some((provider) => provider.id === activeFilter)) {
      setActiveFilter("all");
    }
  }, [activeFilter, providers]);

  const keyword = query.trim().toLowerCase();
  const filtered = useMemo(() => orderedModels.filter((model) => {
    if (activeFilter === "favorites" && !favorites.has(model.ref)) return false;
    if (activeFilter !== "all" && activeFilter !== "favorites" && model.provider !== activeFilter) return false;
    return !keyword
      || model.model.toLowerCase().includes(keyword)
      || model.provider.toLowerCase().includes(keyword)
      || (model.displayName ?? "").toLowerCase().includes(keyword);
  }), [activeFilter, favorites, keyword, orderedModels]);

  // Preserve each provider's model order and apply the user's provider order.
  const groups = useMemo(() => {
    if (activeFilter === "favorites") {
      return [{ id: "favorites", label: t("modelSwitcher.favorites"), items: filtered }];
    }
    if (activeFilter !== "all") {
      const provider = providers.find((item) => item.id === activeFilter);
      return [{ id: activeFilter, label: provider?.label || activeFilter, items: filtered }];
    }
    const favoriteItems = filtered.filter((model) => favorites.has(model.ref));
    const otherItems = filtered.filter((model) => !favorites.has(model.ref));
    return [
      { id: "favorites", label: t("modelSwitcher.favorites"), items: favoriteItems },
      { id: "all", label: t("modelSwitcher.allModels"), items: otherItems },
    ].filter((group) => group.items.length > 0);
  }, [activeFilter, favorites, filtered, providers, t]);

  const currentProvider = useMemo(() => {
    const cur = models.find((m) => m.current) ?? models.find((m) => m.model === label || m.ref === label);
    return cur ? (cur.displayName?.trim() || providerLabel(cur.provider, t)) : null;
  }, [label, models, t]);
  const triggerLabel = [label, currentProvider, detailLabel].filter(Boolean).join(" · ");

  const toggleFavorite = (ref: string) => {
    setFavorites((current) => {
      const next = new Set(current);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      writeModelFavorites(next);
      return next;
    });
  };

  const pick = (model: ModelInfo) => {
    setOpen(false);
    const pendingKey = draftId ? `draft:${draftId}` : tabId ?? "";
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
      void loadModelsForTab(tabId, draftId);
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
      <Tooltip label={triggerLabel} fill disabled={open}>
        <button
          ref={triggerRef}
          type="button"
          className="modelsw__trigger"
          disabled={disabled}
          aria-label={triggerLabel}
          aria-expanded={open && !disabled}
          onClick={() => setOpen((v) => !v)}
        >
          <Cpu size={14} className="modelsw__kind" />
          <span className="modelsw__label">{label}{detailLabel && <span className="modelsw__detail"> · {detailLabel}</span>}{application?.application==="pending" && <span className="modelsw__detail"> · {t("modelApply.badge")}</span>}</span>
          <ChevronDown size={12} />
        </button>
      </Tooltip>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className={`modelsw__menu modelsw__menu--portal${composerMenu ? " composer-menu-surface" : ""}`}
        style={composerMenu ? undefined : { minWidth: Math.max(triggerWidth || 200, 200), maxWidth: "min(90vw, 480px)" }}
      >
        <div className="modelsw__search" role="presentation">
            <Search size={17} />
            <input
              ref={inputRef}
              type="text"
              className="modelsw__search-input"
              placeholder={t("modelSwitcher.searchPlaceholder")}
              aria-label={t("modelSwitcher.searchPlaceholder")}
              value={query}
              onChange={(e) => {
                const nextQuery = e.target.value;
                setQuery(nextQuery);
                // The search field belongs to the whole catalog. Typing while
                // a provider or Favorites is selected must still find models
                // from every configured connection.
                if (nextQuery.trim()) setActiveFilter("all");
              }}
              onKeyDown={(e) => {
                if (e.key === "Escape") setOpen(false);
                if (e.key === "Enter" && filtered.length === 1) pick(filtered[0]);
              }}
            />
        </div>
        <div className="modelsw__body">
          <nav className="modelsw__rail" aria-label={t("modelSwitcher.filters")}>
            <button type="button" className="modelsw__rail-item" aria-label={t("modelSwitcher.favorites")} title={t("modelSwitcher.favorites")} aria-pressed={activeFilter === "favorites"} onClick={() => setActiveFilter("favorites")}>
              <Star size={18} />
            </button>
            <button type="button" className="modelsw__rail-item" aria-label={t("modelSwitcher.allModels")} title={t("modelSwitcher.allModels")} aria-pressed={activeFilter === "all"} onClick={() => setActiveFilter("all")}>
              <List size={19} />
            </button>
            {providers.length > 0 && <span className="modelsw__rail-divider" aria-hidden="true" />}
            {providers.map((provider) => (
              <button key={provider.id} type="button" className="modelsw__rail-item" aria-label={provider.label} title={provider.label} aria-pressed={activeFilter === provider.id} onClick={() => setActiveFilter(provider.id)}>
                <ProviderMark provider={provider.id} label={provider.label} />
              </button>
            ))}
          </nav>
          <div className="modelsw__catalog" role="listbox" aria-label={t("modelSwitcher.modelList")}>
            {models.length === 0 && <div className="modelsw__empty">{t("status.noModels")}</div>}
            {models.length > 0 && filtered.length === 0 && <div className="modelsw__empty">{activeFilter === "favorites" && !query ? t("modelSwitcher.noFavorites") : t("modelSwitcher.noMatches")}</div>}
            {groups.map((g) => (
            <div key={g.id} role="group" aria-label={g.label} className="modelsw__group">
              <div className="modelsw__group-label" role="presentation">{g.label}</div>
              {g.items.map((m) => {
                const favorite = favorites.has(m.ref);
                const favoriteLabel = t(favorite ? "modelSwitcher.removeFavorite" : "modelSwitcher.addFavorite", { model: m.model });
                return (
                <div className="modelsw__row" key={m.ref}>
                <button
                  type="button"
                  role="option"
                  aria-selected={m.current}
                  className={`modelsw__item ${m.current ? "modelsw__item--current" : ""}`}
                  onClick={() => pick(m)}
                >
                  <ProviderMark provider={m.provider} label={m.displayName?.trim() || providerLabel(m.provider, t)} />
                  <span className="modelsw__copy">
                    <span className="modelsw__model">{m.model}</span>
                    <span className="modelsw__meta">{modelMeta(m, t)}</span>
                  </span>
                  {m.contextWindow ? <span className="badge badge--neutral">{formatTokens(m.contextWindow)}</span> : null}
                  {m.vision && <span className="modelsw__capability" title={t("providerUI.image")}><Image size={13} aria-hidden="true" /><span>{t("providerUI.image")}</span></span>}
                  {m.current && <Check size={13} className="modelsw__check" />}
                </button>
                <button type="button" className={`modelsw__favorite${favorite ? " modelsw__favorite--active" : ""}`} aria-label={favoriteLabel} title={favoriteLabel} aria-pressed={favorite} onClick={() => toggleFavorite(m.ref)}>
                  <Star size={16} fill={favorite ? "currentColor" : "none"} />
                </button>
                </div>
              );})}
            </div>
          ))}
          </div>
        </div>
        {details && <div className="modelsw__details">{details}</div>}
        {onManage && <button className="modelsw__manage" type="button" onClick={() => { setOpen(false); onManage(); }}><Plus size={16} />{t("modelSwitcher.configureModels")}<ChevronRight size={15} /></button>}
      </AnchoredPopover>
    </div>
  );
}

export function normalizeModelInfo(model: ModelInfo): ModelInfo {
  return {
    ...model,
    provider: String(model.provider ?? ""),
    model: String(model.model ?? ""),
  };
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

function modelMeta(model: ModelInfo, t: ReturnType<typeof useT>): string {
  const provider = model.displayName?.trim() || providerLabel(model.provider, t);
  return model.current ? `${provider} · ${t("modelSwitcher.currentModel")}` : provider;
}

function providerBrandID(provider: string): string {
  const normalized = provider.trim().toLowerCase();
  if (providerBrandIcons.has(normalized)) return normalized;
  if (normalized.includes("deepseek")) return "deepseek";
  if (normalized.includes("openai") || normalized.includes("gpt")) return "openai";
  if (normalized.includes("anthropic") || normalized.includes("claude")) return "anthropic";
  if (normalized.includes("google") || normalized.includes("gemini")) return "gemini";
  if (normalized.includes("glm") || normalized.includes("zhipu") || normalized.includes("zai")) return "zai";
  if (normalized.includes("minimax")) return "minimax";
  if (normalized.includes("qwen") || normalized.includes("dashscope")) return "qwen";
  if (normalized.includes("kimi") || normalized.includes("moonshot")) return "kimi";
  if (normalized.includes("xai") || normalized.includes("grok")) return "xai";
  return "";
}

function ProviderMark({ provider, label }: { provider: string; label: string }) {
  const brandID = providerBrandID(provider);
  if (brandID) {
    const icon = `url(/provider-icons/${brandID}.svg)`;
    return <span className="modelsw__provider-icon" aria-hidden="true" style={{ maskImage: icon, WebkitMaskImage: icon }} />;
  }
  const monogram = label.trim().match(/[\p{L}\p{N}]/u)?.[0]?.toUpperCase() || "•";
  return <span className="modelsw__provider-monogram" aria-hidden="true">{monogram}</span>;
}
