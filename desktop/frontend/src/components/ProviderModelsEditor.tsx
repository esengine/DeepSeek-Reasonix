import { useProviderT as useT } from "../lib/providerSettingsLocale";
import { useEffect, useRef, useState } from "react";
import { Pencil, Plus, Plug, RefreshCw, Search, Trash2 } from "lucide-react";

import type { ProviderModelCapabilityView, ProviderModelOverrideView, ProviderView } from "../lib/types";
import { applyModelDraft, modelDraftError, type ModelDraft } from "../lib/providerModelDraft";
import { providerVisionModelsForView } from "../lib/providerVisionCapability";
import { ModelImageInputControl } from "./ModelImageInputControl";
import { imageInputModeForModel, imageInputModes, mergeImageInputModes, modelCapabilityForModel, matchingModelKey } from "../lib/providerImageInput";
import { ProviderDialog } from "./ProviderDialog";

export function ProviderModelsEditor({ provider, disabled, canFetch, onChange, onFetch, onTest, probeKey, draft = false }: {
  provider: ProviderView; probeKey?: string; disabled: boolean; canFetch: boolean; draft?: boolean;
  onChange: (models: string[], overrides: ProviderModelOverrideView[], capabilities: ProviderModelCapabilityView[]) => void;
  onFetch: () => Promise<ProviderModelCapabilityView[]>;
  onTest: (model: string) => Promise<void>;
}) {
  const t = useT();
  const [editor, setEditor] = useState<{ original?: string; value: ModelDraft } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [discovery, setDiscovery] = useState<ProviderModelCapabilityView[] | null>(null);
  const [selection, setSelection] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  const [fetching, setFetching] = useState(false);
  const [tests, setTests] = useState<Record<string, { busy: boolean; error?: string }>>({});
  // An endpoint/key/parameter edit invalidates every in-flight result for this draft.
  const identity = JSON.stringify([provider, probeKey]);
  const identityRef = useRef(identity); identityRef.current = identity;
  const epoch = useRef(0);
  useEffect(() => { epoch.current += 1; setFetching(false); setTests({}); setDiscovery(null); setError(null); }, [identity]);
  useEffect(() => () => { epoch.current += 1; }, []);
  const filteredDiscovery = discovery?.filter((item) => item.model.toLowerCase().includes(query.trim().toLowerCase())) ?? [];
  const overrides = provider.modelOverrides ?? [];
  const capabilities = provider.modelCapabilities ?? [];
  const vision = providerVisionModelsForView(provider);
  const openEditor = (model?: string) => {
    const override = overrides.find((item) => item.model === matchingModelKey(overrides.map((item) => item.model), model ?? ""));
    setError(null);
    setEditor({ original: model, value: {
      model: model ?? "", context: override?.contextWindow ? String(override.contextWindow) : "",
      output: override?.maxOutputTokens ? String(override.maxOutputTokens) : "",
      vision: override?.vision == null ? "auto" : override.vision ? "yes" : "no",
    } });
  };
  const fetchModels = async () => {
    const generation = epoch.current, fingerprint = identity;
    setFetching(true); setError(null);
    try {
      const items = await onFetch();
      if (generation !== epoch.current || identityRef.current !== fingerprint) return;
      setDiscovery(Array.from(new Map(items.map((item) => [item.model, item])).values()));
      setSelection([]); setQuery("");
    } catch (e) {
      if (generation === epoch.current && identityRef.current === fingerprint) setError(String((e as Error).message ?? e));
    } finally { if (generation === epoch.current && identityRef.current === fingerprint) setFetching(false); }
  };
  const test = async (model: string) => {
    const generation = epoch.current, fingerprint = identity;
    setTests((prev) => ({ ...prev, [model]: { busy: true } }));
    try {
      await onTest(model);
      if (generation === epoch.current && identityRef.current === fingerprint) setTests((prev) => ({ ...prev, [model]: { busy: false } }));
    } catch (e) {
      if (generation === epoch.current && identityRef.current === fingerprint) setTests((prev) => ({ ...prev, [model]: { busy: false, error: String((e as Error).message ?? e) } }));
    }
  };
  const saveModel = () => {
    if (!editor) return;
    const invalid = modelDraftError(editor.value, provider.models, editor.original);
    if (invalid) { setError(t(`providerUI.validation.${invalid}`)); return; }
    const model = editor.value.model.trim();
    const models = editor.original ? provider.models.map((name) => name === editor.original ? model : name) : [...provider.models, model];
    // Discovery describes a remote ID; it cannot be transferred to a renamed ID.
    const nextCapabilities = model !== editor.original ? capabilities.filter((item) => item.model !== editor.original) : capabilities;
    onChange(models, applyModelDraft(overrides, editor.value, editor.original), nextCapabilities);
    setEditor(null); setError(null);
  };
  return <section className="provider-models-editor">
    <div className="provider-models-editor__head"><strong>{t("settings.modelList")}</strong><div>
      <button type="button" className="btn btn--small" disabled={disabled || fetching || !canFetch} onClick={() => void fetchModels()}><RefreshCw size={14} className={fetching ? "provider-spinning" : undefined} />{t(fetching ? "settings.fetchingModels" : "settings.fetchModels")}</button>
      <button type="button" className="btn btn--small" disabled={disabled} onClick={() => openEditor()}><Plus size={14} />{t("providerUI.manualAdd")}</button>
    </div></div>
    {!editor && error && <p role="alert" className="provider-fetch-status provider-fetch-status--warn">{error}</p>}
    {!provider.models.length && <p className="provider-models-editor__empty">{t("providerUI.emptyModels")}</p>}
    <div className="provider-models-editor__list">
      {provider.models.map((model) => {
        const override = overrides.find((item) => item.model === matchingModelKey(overrides.map((item) => item.model), model ?? ""));
        const result = tests[model];
        return <div className="provider-model-row" key={model}>
          <div className="provider-model-row__main"><span className="provider-model-row__name">{model}</span>
            <span className="badge badge--neutral" title={t("providerUI.context")}>{override?.contextWindow ? override.contextWindow.toLocaleString() : t("providerUI.auto")}</span>
            <span className="badge badge--neutral">{t(vision.includes(model) ? "providerUI.image" : "providerUI.text")}</span>
            <button type="button" className="btn btn--small" aria-label={`${t("providerUI.test")} ${model}`} title={t("providerUI.test")} disabled={disabled || !canFetch || result?.busy} onClick={() => void test(model)}><Plug size={14} /></button>
            <button type="button" className="btn btn--small" aria-label={`${t("providerUI.editModel")} ${model}`} title={t("providerUI.editModel")} disabled={disabled} onClick={() => openEditor(model)}><Pencil size={14} /></button>
            <button type="button" className="btn btn--small" aria-label={`${t("common.delete")} ${model}`} title={t("common.delete")} disabled={disabled} onClick={() => onChange(provider.models.filter((name) => name !== model), overrides.filter((item) => item.model !== model), capabilities.filter((item) => item.model !== model))}><Trash2 size={14} /></button>
          </div>
          <ModelImageInputControl model={model} baseURL={provider.baseUrl} capability={modelCapabilityForModel(capabilities, model)} mode={imageInputModeForModel(imageInputModes(overrides), model)} disabled={disabled}
            onChange={(mode) => onChange(provider.models, mergeImageInputModes(overrides, provider.models, { ...imageInputModes(overrides), [model]: mode }), capabilities)} />
          {result && <div role="status" className={`provider-fetch-status provider-fetch-status--${result.error ? "warn" : "ok"}`}>{result.busy ? t("providerUI.testing") : result.error || t("providerUI.testSuccess")}</div>}
        </div>;
      })}
    </div>
    {editor && <ProviderDialog title={t(editor.original ? "providerUI.editModel" : "providerUI.addModel")} onClose={() => { setEditor(null); setError(null); }}>
      <form onSubmit={(e) => { e.preventDefault(); saveModel(); }}>
        <label className="provider-field">{t("providerUI.modelID")}<input className="mem-input" value={editor.value.model} onChange={(e) => setEditor({ ...editor, value: { ...editor.value, model: e.target.value } })} /></label>
        <div className="provider-field-grid">{(["context", "output"] as const).map((field) => <label className="provider-field" key={field}>{t(field === "context" ? "providerUI.context" : "providerUI.output")}<input className="mem-input" inputMode="numeric" placeholder={t("providerUI.auto")} value={editor.value[field]} onChange={(e) => setEditor({ ...editor, value: { ...editor.value, [field]: e.target.value } })} /><small>{t(field === "output" ? "providerUI.outputHint" : "providerUI.inherit")}</small></label>)}</div>
        <ModelImageInputControl model={editor.value.model} baseURL={provider.baseUrl} capability={modelCapabilityForModel(capabilities, editor.value.model)}
          mode={editor.value.vision === "yes" ? "on" : editor.value.vision === "no" ? "off" : "auto"} disabled={disabled}
          onChange={(mode) => setEditor({ ...editor, value: { ...editor.value, vision: mode === "on" ? "yes" : mode === "off" ? "no" : "auto" } })} />
        {error && <p role="alert" className="provider-fetch-status provider-fetch-status--warn">{error}</p>}
        <footer><button type="button" className="btn btn--small" onClick={() => { setEditor(null); setError(null); }}>{t("common.cancel")}</button><button type="submit" className="btn btn--primary btn--small" disabled={disabled}>{t(draft ? "providerUI.saveDraft" : "providerUI.applyModel")}</button></footer>
      </form>
    </ProviderDialog>}
    {discovery && <ProviderDialog title={t("settings.fetchModels")} onClose={() => setDiscovery(null)}>
      <label className="provider-catalog-search"><Search size={15} /><input className="mem-input" placeholder={t("settings.modelCandidateSearch")} value={query} onChange={(e) => setQuery(e.target.value)} /></label>
      <div className="provider-discovery-list">{filteredDiscovery.map((item) => <label key={item.model} className="provider-discovery-row"><input type="checkbox" disabled={provider.models.includes(item.model)} checked={provider.models.includes(item.model) || selection.includes(item.model)} onChange={(e) => setSelection((prev) => e.target.checked ? [...prev, item.model] : prev.filter((name) => name !== item.model))} /><span>{item.model}</span>{provider.models.includes(item.model) && <small>{t("providerUI.alreadyAdded")}</small>}</label>)}{!filteredDiscovery.length && <p>{t(discovery.length ? "settings.noMatchingCandidateModels" : "providerUI.noDiscoveredModels")}</p>}</div>
      <p className="mem-hint">{t("providerUI.discoveryHint")}</p>
      <footer><span>{t("providerUI.selected", { n: selection.length })}</span><button className="btn btn--small" onClick={() => setDiscovery(null)}>{t("common.cancel")}</button><button className="btn btn--primary btn--small" disabled={disabled || !selection.length} onClick={() => {
        const added = selection.filter((name) => !provider.models.includes(name));
        onChange([...provider.models, ...added], overrides, [...capabilities.filter((item) => !added.includes(item.model)), ...discovery.filter((item) => added.includes(item.model))]);
        setDiscovery(null);
      }}>{t("providerUI.addSelected")}</button></footer>
    </ProviderDialog>}
  </section>;
}
