import { Children, Fragment, isValidElement, useEffect, useId, useMemo, useRef, useState } from "react";
import type { ButtonHTMLAttributes, KeyboardEvent, ReactNode } from "react";
import { Check, ChevronDown } from "lucide-react";
import { AnchoredPopover } from "./AnchoredPopover";

if (import.meta.env?.MODE) void import("./SettingsSelect.css");

export type SettingsSelectOption = {
  value: string;
  label: string;
  hint?: string;
  group?: string;
  groupLabel?: string;
  searchText?: string;
  disabled?: boolean;
};

function optionChildren(children: ReactNode, group?: string): SettingsSelectOption[] {
  return Children.toArray(children).flatMap(child => {
    if (!isValidElement<{ children?: ReactNode; value?: string | number; label?: string; disabled?: boolean }>(child)) return [];
    if (child.type === Fragment) return optionChildren(child.props.children, group);
    if (child.type === "optgroup") return optionChildren(child.props.children, child.props.label).map(option => ({ ...option, disabled: child.props.disabled || option.disabled }));
    if (child.type !== "option") return [];
    const label = Children.toArray(child.props.children).join("");
    return [{ value: String(child.props.value ?? label), label, group, disabled: child.props.disabled }];
  });
}

type Props = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "value" | "onChange" | "children" | "onClick"> & {
  value: string | number;
  onValueChange: (value: string) => void;
  children?: ReactNode;
  options?: SettingsSelectOption[];
  searchPlaceholder?: string;
  emptyLabel?: string;
  selectedLabel?: string;
};

/** One compact selection surface for settings, including searchable model catalogs. */
export function SettingsSelect({ value, onValueChange, children, options, searchPlaceholder, emptyLabel,
  selectedLabel, className = "", disabled, name, onKeyDown, ...buttonProps }: Props) {
  const id = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState<string | null>(null);
  useEffect(() => { if (disabled) setOpen(false); }, [disabled]);
  const items = useMemo(() => options ?? optionChildren(children), [options, children]);
  const selected = items.find(option => option.value === String(value));
  const visible = useMemo(() => {
    const q = query.trim().toLocaleLowerCase();
    return items.filter(option => !q || [option.label, option.hint, option.groupLabel, option.group, option.searchText].join(" ").toLocaleLowerCase().includes(q));
  }, [items, query]);
  const enabled = visible.filter(option => !option.disabled);
  const activeValue = enabled.some(option => option.value === active) ? active : enabled[0]?.value;
  const activeIndex = visible.findIndex(option => option.value === activeValue);
  const activeID = activeIndex < 0 ? undefined : `${id}-option-${activeIndex}`;
  const show = () => {
    setQuery("");
    setActive(selected && !selected.disabled ? selected.value : null);
    setOpen(true);
  };
  const pick = (next: string) => {
    setOpen(false);
    triggerRef.current?.focus();
    if (next !== String(value)) onValueChange(next);
  };
  const navigate = (event: KeyboardEvent<HTMLElement>) => {
    // Candidate navigation/confirmation belongs to the IME, including WebKit's 229 fallback.
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) {
      event.stopPropagation();
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
    } else if (event.key === "Tab") {
      setOpen(false);
      triggerRef.current?.focus();
    } else if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      // Home/End retain text-editing behavior in the search input.
      if (event.currentTarget.tagName === "INPUT" && ["Home", "End"].includes(event.key)) return;
      event.preventDefault();
      if (!enabled.length) return;
      const current = enabled.findIndex(option => option.value === activeValue);
      const next = event.key === "Home" ? 0 : event.key === "End" ? enabled.length - 1
        : (current + (event.key === "ArrowDown" ? 1 : -1) + enabled.length) % enabled.length;
      const option = enabled[next];
      setActive(option.value);
      document.getElementById(`${id}-option-${visible.indexOf(option)}`)?.scrollIntoView({ block: "nearest" });
    } else if (event.key === "Enter" || (event.key === " " && !searchPlaceholder)) {
      event.preventDefault();
      if (activeValue != null) pick(activeValue);
    } else if (!searchPlaceholder && event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
      const match = enabled.find(option => option.label.toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()));
      if (match) setActive(match.value);
    }
  };

  return <>
    {name && <input type="hidden" name={name} value={value} disabled={disabled} />}
    <button {...buttonProps} ref={triggerRef} type="button" value={value} disabled={disabled}
      id={buttonProps.id ?? `${id}-trigger`} className={`settings-select ${className}`} aria-haspopup="listbox" aria-expanded={open && !disabled}
      aria-controls={open ? `${id}-list` : undefined} onClick={() => open ? setOpen(false) : show()}
      onKeyDown={event => {
        onKeyDown?.(event);
        if (!event.defaultPrevented && ["ArrowDown", "ArrowUp"].includes(event.key)) { event.preventDefault(); show(); }
      }}>
      <span className="settings-select__value" title={selected?.hint}>{selectedLabel ?? selected?.label ?? String(value)}</span>
      <ChevronDown size={14} aria-hidden="true" />
    </button>
    <AnchoredPopover open={open && !disabled} anchorRef={triggerRef} onClose={() => setOpen(false)}
      className="settings-select-menu" placement="bottom" offset={4}
      style={{ width: triggerRef.current?.getBoundingClientRect().width }}>
      {searchPlaceholder && <div className="settings-select-menu__search"><input autoFocus value={query}
        role="combobox" aria-expanded="true" aria-controls={`${id}-list`} aria-activedescendant={activeID}
        aria-autocomplete="list" aria-label={searchPlaceholder} placeholder={searchPlaceholder}
        onChange={event => { setQuery(event.target.value); setActive(null); }} onKeyDown={navigate} /></div>}
      <div id={`${id}-list`} role="listbox" aria-label={buttonProps["aria-label"]}
        aria-labelledby={buttonProps["aria-label"] ? undefined : (buttonProps["aria-labelledby"] ?? buttonProps.id ?? `${id}-trigger`)} tabIndex={searchPlaceholder ? -1 : 0}
        aria-activedescendant={searchPlaceholder ? undefined : activeID} onKeyDown={navigate}
        ref={node => { if (node && !searchPlaceholder && open) node.focus(); }}
        className="settings-select-menu__list">
        {visible.map((option, index) => <Fragment key={option.value}>
          {option.group && option.group !== visible[index - 1]?.group && <div className="settings-select-menu__group">{option.groupLabel ?? option.group}</div>}
          <div id={`${id}-option-${index}`} role="option" data-value={option.value} aria-selected={option.value === String(value)}
            aria-disabled={option.disabled || undefined} title={option.hint || option.label}
            className={`settings-select-menu__option${option.value === activeValue ? " is-active" : ""}`}
            onMouseDown={event => event.preventDefault()}
            onMouseMove={() => !option.disabled && setActive(option.value)}
            onClick={() => !option.disabled && pick(option.value)}>
            <span>{option.label}</span>{option.value === String(value) && <Check size={14} aria-hidden="true" />}
          </div>
        </Fragment>)}
        {!visible.length && <div className="settings-select-menu__empty" role="status">{emptyLabel}</div>}
      </div>
    </AnchoredPopover>
  </>;
}
