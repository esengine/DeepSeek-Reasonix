import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Brain, Check, ChevronsUpDown, Search } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ModelInfo } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";

// ModelSwitcher opens an upward popover listing configured providers. Selecting
// one switches the active model while the current conversation continues.
export function ModelSwitcher({ label, tabId, onPick }: { label: string; tabId?: string; onPick: (name: string) => void }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [query, setQuery] = useState("");
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>(undefined);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

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

  const loadModels = useCallback(() => {
    return (tabId ? app.ModelsForTab(tabId) : app.Models()).then((next) => setModels(asArray(next))).catch(() => {});
  }, [tabId]);

  useEffect(() => {
    void loadModels();
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

  const pick = (name: string) => {
    setModels((prev) => prev.map((m) => ({ ...m, current: m.ref === name })));
    setOpen(false);
    onPick(name);
  };

  return (
    <div className="modelsw">
      <button
        ref={triggerRef}
        type="button"
        className="modelsw__trigger"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <Brain size={13} className="modelsw__kind" />
        <span className="modelsw__label">{label}{currentProvider ? ` · ${currentProvider}` : ""}</span>
        <ChevronsUpDown size={11} />
      </button>
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
                if (e.key === "Enter" && filtered.length === 1) pick(filtered[0].ref);
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
                  onClick={() => pick(m.ref)}
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
    case "mimo-api":
    case "mimo":
    case "xiaomi-mimo":
      return t("settings.providerLabel.mimoApi");
    case "mimo-token-plan":
    case "mimo-pro":
    case "mimo-flash":
      return t("settings.providerLabel.mimoTokenPlan");
    default:
      return provider;
  }
}
