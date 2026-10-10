import { useState } from "react";
import { t } from "../i18n";
import type { ModelEffort } from "../port/port";
import { parseEffortLevels } from "./provider_compat";

// The wire vocabulary in depth order, so chips read shallow to deep whatever
// order a declaration was stored in. Levels outside it follow in their own order.
const DEPTH = ["none", "disabled", "minimal", "low", "medium", "high", "xhigh", "max"];

// A protocol with a fixed vocabulary, or none at all, leaves a declared list
// on file with nothing to act on.
export const dormantProtocol = (protocol: string) => protocol === "kimi-k3" || protocol === "none";

// One row per enabled model: inherit the connection's levels, or pick the
// model's own. Only a model with its own levels carries an entry in value.
export function ModelEfforts({
  models, value, onChange, inherited, protocols, connProtocol,
}: {
  models: string[];
  value: Record<string, ModelEffort>;
  onChange: (next: Record<string, ModelEffort>) => void;
  // What each model runs on when it declares nothing: the connection's typed
  // list, else the kernel's answer for the saved config. null is an answer
  // that waits on a connection change not yet saved.
  inherited: (model: string) => ModelEffort | null | undefined;
  protocols: Record<string, string>;
  connProtocol: string;
}) {
  const set = (model: string, next: ModelEffort | undefined) => {
    const out = { ...value };
    if (next) out[model] = next;
    else delete out[model];
    onChange(out);
  };
  return (
    <div className="grow full model-efforts" role="group" aria-label={t("按模型设置档位")}>
      <span>{t("按模型设置档位")}</span>
      <i className="tip">{t("同一接入下有多家厂商的模型时，可为单个模型指定档位；「继承接入设置」沿用上面的档位。模型自己的档位优先。")}</i>
      <ul className="me-list">
        {models.map((model) => {
          const own = value[model];
          const from = inherited(model);
          const dormant = dormantProtocol(protocols[model] || connProtocol);
          return (
            <li key={model} className="me-row" data-model={model}>
              <span className="me-name" title={model}>{model}</span>
              <select
                aria-label={t("{model} 的推理档位", { model })}
                data-action="provider.draft" data-value="model-effort-mode"
                value={own ? "own" : "inherit"}
                disabled={dormant}
                onChange={(e) => set(model, e.target.value === "own"
                  ? { supportedEfforts: from?.supportedEfforts ?? [], defaultEffort: seededDefault(from) }
                  : undefined)}
              >
                <option value="inherit">{from?.official ? t("继承官方档位") : t("继承接入设置")}</option>
                <option value="own">{t("单独设置")}</option>
              </select>
              {dormant ? (
                <i className="tip me-note">{t("该模型的思考参数不使用自定义档位。")}</i>
              ) : own ? (
                <OwnLevels model={model} own={own} from={from} onChange={(next) => set(model, next)} />
              ) : (
                <i className="tip me-note">
                  {from === null
                    ? t("继承：保存后按接入设置确定")
                    : from?.official
                      ? officialNote(from)
                      : from?.supportedEfforts.length
                        ? t("继承：{levels}", { levels: from.supportedEfforts.join(" · ") })
                        : t("继承：端点未声明档位")}
                </i>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function OwnLevels({ model, own, from, onChange }: {
  model: string; own: ModelEffort; from?: ModelEffort | null; onChange: (next: ModelEffort) => void;
}) {
  const [extra, setExtra] = useState("");
  const levels = own.supportedEfforts;
  const offered = candidates(levels, from?.supportedEfforts ?? []);
  const toggle = (level: string) => {
    const picked = levels.includes(level) ? levels.filter((l) => l !== level) : [...levels, level];
    onChange(withDefault(ordered(picked, offered), own.defaultEffort));
  };
  const addTyped = () => {
    const typed = parseEffortLevels(extra);
    if (typed.length === 0) return;
    onChange(withDefault([...levels, ...typed.filter((l) => !levels.includes(l))], own.defaultEffort));
    setExtra("");
  };
  return (
    <div className="me-own">
      <div className="me-chips">
        {offered.map((level) => (
          <button key={level} type="button" className="chip" aria-pressed={levels.includes(level)}
            data-action="provider.draft" data-value="model-effort-level" onClick={() => toggle(level)}>
            {level}
          </button>
        ))}
        <input className="me-extra" data-action-change="provider.draft" data-action-keydown="provider.draft"
          data-value="model-effort-extra" value={extra} spellCheck={false} placeholder={t("其他档位")}
          aria-label={t("{model} 的其他档位", { model })}
          onChange={(e) => setExtra(e.target.value)} onBlur={addTyped}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addTyped(); } }} />
      </div>
      <label className="me-default">
        <span>{t("默认档位")}</span>
        <select aria-label={t("{model} 的默认档位", { model })} value={own.defaultEffort ?? ""}
          data-action="provider.draft" data-value="model-default-effort"
          disabled={levels.length === 0}
          onChange={(e) => onChange({ supportedEfforts: levels, defaultEffort: e.target.value })}>
          <option value="">{levels[0] ? t("第一个档位（{level}）", { level: levels[0] }) : t("第一个档位")}</option>
          {levels.map((l) => <option key={l} value={l}>{l}</option>)}
        </select>
      </label>
      {levels.length === 0 && <i className="tip me-note">{t("未选档位时按继承处理。")}</i>}
    </div>
  );
}

function officialNote(from: ModelEffort): string {
  const levels = from.supportedEfforts.join(" · ");
  return from.defaultEffort
    ? t("官方档位：{levels}（默认 {def}）", { levels, def: from.defaultEffort })
    : t("官方档位：{levels}", { levels });
}

function seededDefault(from?: ModelEffort | null): string {
  const def = from?.defaultEffort ?? "";
  return from?.supportedEfforts.includes(def) ? def : "";
}

function withDefault(levels: string[], def?: string): ModelEffort {
  return { supportedEfforts: levels, defaultEffort: def && levels.includes(def) ? def : "" };
}

function candidates(own: string[], inherited: string[]): string[] {
  const out = [...DEPTH];
  for (const level of [...inherited, ...own]) if (!out.includes(level)) out.push(level);
  return out;
}

function ordered(levels: string[], offered: string[]): string[] {
  return offered.filter((l) => levels.includes(l));
}
