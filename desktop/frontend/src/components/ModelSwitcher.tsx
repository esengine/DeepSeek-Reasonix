import { useEffect, useRef, useState } from "react";
import { Brain, Check, ChevronsUpDown } from "lucide-react";
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
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (open) {
      (tabId ? app.ModelsForTab(tabId) : app.Models()).then((next) => setModels(asArray(next))).catch(() => {});
    }
  }, [open, tabId]);

  const pick = (name: string) => {
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
        <span className="modelsw__label">{label}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="modelsw__menu modelsw__menu--portal"
      >
        <div role="listbox">
          {models.length === 0 && <div className="modelsw__empty">{t("status.noModels")}</div>}
          {models.map((m) => (
            <button
              key={m.ref}
              type="button"
              role="option"
              aria-selected={m.current}
              className={`modelsw__item ${m.current ? "modelsw__item--current" : ""}`}
              onClick={() => pick(m.ref)}
            >
              <span className="modelsw__model">{m.model}</span>
              {m.current && <Check size={13} className="modelsw__check" />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}
