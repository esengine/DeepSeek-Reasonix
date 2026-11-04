import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { CompositionChoice, CompositionKind, CompositionModule } from "../port/composition";
import { StudioIcon } from "./StudioIcon";
import { useDismiss } from "./dismiss";
import { beside, pinToViewport, useFollow, zoom } from "./place";

interface Props {
  kind: CompositionKind;
  label: string;
  choices: CompositionChoice[];
  members: CompositionModule[];
  disabled: boolean;
  onChange: (members: CompositionModule[]) => void;
}

export function CompositionModulePicker({ kind, label, choices, members, disabled, onChange }: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const all = useRef<HTMLInputElement>(null);
  const id = useId();
  const close = useCallback(() => { setOpen(false); trigger.current?.focus(); }, []);
  const visible = open && !disabled;
  const place = useCallback(() => {
    if (!panel.current || !trigger.current) return;
    const rect = trigger.current.getBoundingClientRect();
    pinToViewport(panel.current, rect.left, beside(rect, panel.current.offsetHeight * zoom(), innerHeight, "below", 6), 12);
  }, []);
  useFollow(visible, place, query, { anchor: trigger, container: panel });
  useDismiss(visible, panel, close, trigger);
  useEffect(() => { if (disabled) setOpen(false); }, [disabled]);
  useEffect(() => { if (visible) search.current?.focus(); }, [visible]);
  useEffect(() => {
    if (!visible || !panel.current) return;
    const observer = new ResizeObserver(place);
    observer.observe(panel.current);
    return () => observer.disconnect();
  }, [visible, place]);
  const selected = new Set(members.map(m => m.id));
  const unique = [...new Map(choices.map(choice => [choice.id, choice])).values()];
  const needle = query.trim().toLocaleLowerCase();
  const shown = unique.filter(choice => `${choice.id} ${choice.description ?? ""}`.toLocaleLowerCase().includes(needle));
  const count = shown.filter(choice => selected.has(choice.id)).length;
  useEffect(() => { if (all.current) all.current.indeterminate = count > 0 && count < shown.length; }, [count, shown.length, visible]);
  const toggle = (items: CompositionChoice[], checked: boolean) => {
    const ids = new Set(items.map(item => item.id));
    onChange(checked ? [...members, ...items.filter(item => !selected.has(item.id)).map(item => ({ id: item.id }))] : members.filter(member => !ids.has(member.id)));
  };
  const navigate = (event: KeyboardEvent<HTMLDivElement>) => {
    const stops = Array.from(panel.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled)") ?? []);
    if (event.key === "Tab") {
      const at = stops.indexOf(document.activeElement as HTMLElement);
      if (event.shiftKey && at === 0 || !event.shiftKey && at === stops.length - 1) {
        event.preventDefault(); (event.shiftKey ? stops.at(-1) : stops[0])?.focus();
      }
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const rows = Array.from(panel.current?.querySelectorAll<HTMLInputElement>(".composition-choice input") ?? []);
    if (!rows.length) return;
    const at = rows.indexOf(document.activeElement as HTMLInputElement);
    event.preventDefault();
    rows[at < 0 ? event.key === "ArrowDown" ? 0 : rows.length - 1 : (at + (event.key === "ArrowDown" ? 1 : -1) + rows.length) % rows.length]?.focus();
  };
  const name = `${label} ${t("persona.add_module")}`;
  return <>
    <button ref={trigger} type="button" className="composition-module-trigger" data-action="agent-preset.module-choose" data-target={kind}
      aria-label={name} aria-haspopup="dialog" aria-expanded={visible} aria-controls={visible ? id : undefined} disabled={disabled}
      onClick={() => { if (visible) close(); else { setQuery(""); setOpen(true); } }}>
      <span>{t("persona.add_module")} · {t("persona.n_selected", { n: members.length })}</span><StudioIcon name="down" />
    </button>
    {visible && createPortal(<div id={id} ref={panel} className="composition-module-picker" role="dialog" aria-label={name} aria-modal="true"
      data-action-keydown="agent-preset.module-navigate" onKeyDown={navigate}>
      <div className="composition-picker-tools">
        <label className="composition-select-all"><input ref={all} type="checkbox" checked={shown.length > 0 && count === shown.length} disabled={!shown.length}
          data-action-change="agent-preset.module-select-all" data-target={kind} onChange={event => toggle(shown, event.target.checked)} />
          {t(needle ? "persona.select_all_results_n" : "persona.select_all_n", { n: shown.length })}</label>
        <button type="button" className="composition-picker-action" disabled={!members.length} data-action="agent-preset.module-clear" data-target={kind} onClick={() => onChange([])}>{t("common.clear")}</button>
      </div>
      <div className="composition-picker-search"><StudioIcon name="search" /><input ref={search} type="search" aria-label={t("persona.search_modules")} placeholder={t("persona.search_module_names_or_descriptions")}
        data-action-change="agent-preset.module-search" data-target={kind} value={query} onChange={event => setQuery(event.target.value)} /></div>
      <div className="composition-picker-list">
        {(["builtin", "custom", "unknown"] as const).map(source => {
          const items = shown.filter(choice => (choice.source === "builtin" || choice.source === "custom" ? choice.source : "unknown") === source);
          if (!items.length) return null;
          const chosen = items.filter(item => selected.has(item.id)).length;
          const group = t(source === "builtin" ? "common.built_in" : source === "custom" ? "common.custom" : "persona.unclassified");
          return <div role="group" aria-label={group} className="composition-picker-group" key={source}>
            <div className="composition-picker-group-heading"><strong>{group}</strong><small>{t("persona.selected_total_selected", { selected: chosen, total: items.length })}</small>
              <button type="button" className="composition-picker-action" data-action="agent-preset.module-select-group" data-target={`${kind}:${source}`}
                onClick={() => toggle(items, chosen !== items.length)}>{t(chosen === items.length ? "persona.deselect_group" : "persona.select_group")}</button></div>
            {items.map((item, index) => <label key={item.id} className="composition-choice">
              <input type="checkbox" aria-label={item.id} aria-describedby={item.description ? `${id}-${source}-${index}` : undefined} checked={selected.has(item.id)}
                data-action-change="agent-preset.module-toggle" data-target={`${kind}:${item.id}`} onChange={event => toggle([item], event.target.checked)} />
              <StudioIcon name={source === "builtin" ? "code" : "layers"} /><span className="composition-choice-name">{item.id}</span>
              {item.description && <span className="composition-choice-description" id={`${id}-${source}-${index}`} title={item.description}>{item.description}</span>}
            </label>)}
          </div>;
        })}
        {!shown.length && <p className="composition-picker-empty">{t(unique.length ? "persona.no_matching_modules" : "persona.no_modules_available")}</p>}
      </div>
      <div className="composition-picker-footer"><span aria-live="polite">{t("persona.n_selected", { n: members.length })}</span>
        <button type="button" className="btn" data-primary="" data-action="agent-preset.module-done" onClick={close}>{t("common.done")}</button></div>
      <p className="composition-picker-note">{t("persona.select_all_includes_current_results")}</p>
    </div>, document.body)}
  </>;
}
