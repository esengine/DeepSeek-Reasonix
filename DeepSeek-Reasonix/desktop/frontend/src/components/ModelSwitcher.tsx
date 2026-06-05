import { useEffect, useState } from "react";
import { Check, ChevronsUpDown } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ModelInfo } from "../lib/types";

// ModelSwitcher is the bottom-of-window model picker: the status line's model
// label becomes a button that opens a popover (upward) listing configured
// providers. Selecting one switches the active model; the conversation is carried
// over by the backend, so the chat continues. Mirrors the "switch model, keep the
// session" behavior of comparable coding agents.
export function ModelSwitcher({ label, onPick }: { label: string; onPick: (name: string) => void }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);

  useEffect(() => {
    if (open) app.Models().then(setModels).catch(() => {});
  }, [open]);

  const pick = (name: string) => {
    setOpen(false);
    onPick(name);
  };

  return (
    <div className="modelsw">
      <button className="modelsw__trigger" onClick={() => setOpen((v) => !v)}>
        <span className="modelsw__label">{label}</span>
        <ChevronsUpDown size={11} />
      </button>
      {open && (
        <>
          <div className="modelsw__backdrop" onClick={() => setOpen(false)} />
          <div className="modelsw__menu" role="listbox">
            {models.length === 0 && <div className="modelsw__empty">{t("status.noModels")}</div>}
            {models.map((m) => (
              <button
                key={m.ref}
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
        </>
      )}
    </div>
  );
}
