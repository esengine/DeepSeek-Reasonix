import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { placeInViewport, useFollow, zoom } from "./place";

export interface MenuItem {
  value: string;
  label: string;
  desc?: string;
  right?: string;
  // Dense choice menus sometimes need one short machine-facing label beside
  // the human one (for example "深入  High") without pushing that label to
  // the far edge of the row.
  meta?: string;
  badge?: string;
  strength?: number;
  recommended?: boolean;
  plain?: boolean;
  divide?: boolean;
  // A group caption. It labels the rows under it and cannot be chosen, so it
  // is not a button — a menu whose headings take focus is a menu you arrow
  // through twice.
  header?: boolean;
  disabled?: boolean;
  // A letter chip leading the row or heading, standing in for a vendor mark.
  mono?: { letter: string; tone: number };
  // A switch rather than a choice: picking it flips this state, and the row
  // says which state it is in instead of being marked as the current value.
  toggle?: boolean;
}

// Where a list stops fitting the menu's own height cap. Past it a scrollbar
// alone still means hunting — one gateway account can publish a hundred
// models — and below it the field would be one more thing to look past.
const FILTER_FROM = 10;

interface Props {
  label: ReactNode;
  items: MenuItem[];
  current?: string;
  onPick: (value: string) => void;
  place: "top" | "bottom";
  // Which edge of the trigger the menu lines up with. It is stated here because
  // the menu no longer sits under a positioned ancestor a stylesheet can reach.
  align?: "start" | "end";
  className?: string;
  title?: string;
  // The choice already made is being applied and has not landed. Marked on the
  // trigger alone: whatever else is on that shelf is still the reader's to
  // change while this one waits.
  pending?: boolean;
  triggerAction?: string;
  ariaPressed?: boolean;
  ariaLabel?: string;
  ariaDescribedBy?: string;
  wrapClassName?: string;
  menuClassName?: string;
  menuTitle?: ReactNode;
  // Opening is when the reader asks what the choices are, so a menu whose rows
  // come from elsewhere re-reads them here rather than showing its mount's copy.
  onOpen?: () => void;
  // The field is the menu's way in rather than its answer to length: a model
  // list is searched for by name however few entries it has.
  searchAlways?: boolean;
  searchPlaceholder?: string;
}

// data-* rides through to the item that raises the pick, the way Switch and Seg
// carry theirs: the action's identity is written at the call site, and the
// answer this menu gives is the item's own value.
export function Picker({
  label, items, current, onPick, place, align = "start", className, title, pending, triggerAction, ariaPressed, ariaLabel, ariaDescribedBy, wrapClassName, menuClassName, menuTitle, onOpen, searchAlways, searchPlaceholder, ...id
}: Props & { [K in `data-${string}`]?: string }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const wrap = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const find = useRef<HTMLInputElement>(null);

  const choosable = items.filter((it) => !it.header && !it.plain).length;
  const filtering = searchAlways || choosable > FILTER_FROM;
  const shown = useMemo(() => (filtering ? matching(items, query) : items), [filtering, items, query]);
  // What Enter takes while the field still has focus. Only shown once the
  // query narrows something, or every menu would open pre-selected.
  const lead = query ? shown.find((it) => !it.header && !it.plain)?.value : undefined;
  const none = filtering && !shown.some((it) => !it.header && !it.plain);

  useEffect(() => {
    if (!open) {
      setQuery("");
      return;
    }
    if (filtering) find.current?.focus();
    else menu.current?.querySelector<HTMLElement>("button.mi")?.focus();
    menu.current?.querySelector<HTMLElement>("button.mi[data-on]")?.scrollIntoView({ block: "nearest" });
    // The menu is not inside the wrapper any more, so without asking it too a
    // press on one of its own rows would read as a press outside.
    const onDown = (e: MouseEvent) => {
      const at = e.target as Node;
      if (!wrap.current?.contains(at) && !menu.current?.contains(at)) setOpen(false);
    };
    // Esc unwinds one layer at a time: the menu first, the run only once no
    // menu is left, so the capture phase has to stop it reaching the app.
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      setOpen(false);
      btn.current?.focus();
    };
    addEventListener("mousedown", onDown);
    addEventListener("keydown", onKey, true);
    return () => {
      removeEventListener("mousedown", onDown);
      removeEventListener("keydown", onKey, true);
    };
  }, [open, filtering]);

  // Measured rather than anchored, because the menu is rendered into the body
  // and has no positioned ancestor left to hang on. It is rendered there so no
  // ancestor can take its z-index down with it: a stacking context anywhere
  // above — an entrance animation's fill transform, a card's paint containment
  // — traps the menu at that ancestor's level and the transcript draws over it.
  const put = useCallback(() => {
    const anchor = btn.current?.getBoundingClientRect();
    const el = menu.current;
    if (!anchor || !el || el.hidden) return;
    // The layout box, not the painted one: the entrance transition is running
    // while this measures, and its scaleY would place the menu by a size it is
    // on its way out of. offsetWidth is in CSS pixels where a client rect is on
    // screen, so it is scaled to meet the anchor's units.
    const s = zoom();
    const box = { width: el.offsetWidth * s, height: el.offsetHeight * s };
    const under = anchor.bottom + 7;
    const over = anchor.top - box.height - 9;
    const down = place === "top";
    const fits = down ? under + box.height <= innerHeight - 6 : over >= 6;
    const at = placeInViewport(
      { x: align === "end" ? anchor.right - box.width : anchor.left, y: fits === down ? under : over },
      box,
      { width: innerWidth, height: innerHeight },
      s,
    );
    el.style.left = `${at.left}px`;
    el.style.top = `${at.top}px`;
  }, [align, place]);

  useFollow(open, put, shown.length);

  // Headings are divs and never take focus, so walking the buttons is what
  // keeps one arrow press from landing on nothing.
  const arrows = (e: React.KeyboardEvent) => {
    const typed = e.key.length === 1 && e.key !== " " && !e.ctrlKey && !e.metaKey && !e.altKey;
    if (filtering && typed && e.target !== find.current) {
      find.current?.focus();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const all = [...(menu.current?.querySelectorAll<HTMLElement>("button.mi") ?? [])];
    if (!all.length) return;
    const i = all.indexOf(document.activeElement as HTMLElement);
    // From the field, Down enters the list at the row Enter would have taken.
    if (i < 0) {
      if (e.key !== "ArrowDown") return;
      e.preventDefault();
      all[0].focus();
      return;
    }
    const to = e.key === "ArrowDown" ? i + 1 : i - 1;
    if (to < 0 && filtering) {
      e.preventDefault();
      find.current?.focus();
      return;
    }
    if (to < 0 || to >= all.length) return;
    e.preventDefault();
    all[to].focus();
  };

  // Taking a value closes the menu the way Escape does, so the focus goes back
  // the same way too. Without it the row that had focus is unmounted under the
  // caret, which drops to the document — and the next Tab restarts at the top
  // of the page, so choosing costs a keyboard user their place and cancelling
  // does not.
  const take = (value: string) => {
    setOpen(false);
    btn.current?.focus();
    onPick(value);
  };

  return (
    <div className={["picker", wrapClassName].filter(Boolean).join(" ")} ref={wrap}>
      <button
        ref={btn}
        className={className}
        data-action={triggerAction}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-pressed={ariaPressed}
        aria-label={ariaLabel}
        aria-describedby={ariaDescribedBy}
        data-pending={pending ? "" : undefined}
        title={title}
        onClick={() => {
          if (!open) onOpen?.();
          setOpen(!open);
        }}
      >
        {label}
      </button>
      {createPortal(
        <div
          ref={menu}
          className={[place === "top" ? "menu portmenu pm-down" : "menu portmenu pm-up", menuClassName].filter(Boolean).join(" ")}
          role="menu"
          hidden={!open}
          onKeyDown={arrows}
        >
          {menuTitle && <div className="studio-picker-title">{menuTitle}</div>}
          {filtering && (
            <input
              ref={find}
              className="mfind"
              value={query}
              placeholder={searchPlaceholder ?? t("筛选 {n} 项", { n: choosable })}
              aria-label={t("筛选")}
              spellCheck={false}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key !== "Enter" || !lead) return;
                e.preventDefault();
                take(lead);
              }}
            />
          )}
          {none && <div className="mnone">{t("没有匹配的项")}</div>}
          {shown.map((it, i) => (
            <Fragment key={it.value}>
              {it.divide && i > 0 && <div className="div" />}
              {it.header ? (
                <div className="mi head">
                  {it.mono && <Mono {...it.mono} />}
                  <span className="lb">{it.label}</span>
                  {it.right && <span className="rt">{it.right}</span>}
                </div>
              ) : (
                <button
                  {...id}
                  data-value={it.value}
                  className={it.plain ? "mi plain" : "mi"}
                  role={it.toggle === undefined ? "menuitem" : "menuitemcheckbox"}
                  aria-checked={it.toggle}
                  disabled={it.disabled}
                  data-on={it.value === current ? "" : undefined}
                  data-lead={it.value === lead ? "" : undefined}
                  data-strength={it.strength}
                  data-recommended={it.recommended ? "" : undefined}
                  onClick={() => take(it.value)}
                >
                  {it.mono ? <Mono {...it.mono} /> : <span className="dot" />}
                  <span className="tx">
                    <span className="studio-item-line">
                      <span className="lb">{it.label}</span>
                      {it.meta && <span className="studio-item-meta">{it.meta}</span>}
                      {it.badge && <span className="studio-item-badge">{it.badge}</span>}
                    </span>
                    {it.desc && <span className="ds">{it.desc}</span>}
                  </span>
                  {it.strength != null && (
                    <span className="studio-effort-strength" aria-hidden="true">
                      {[1, 2, 3, 4].map((level) => <i key={level} data-on={level <= (it.strength ?? 0) ? "" : undefined} />)}
                    </span>
                  )}
                  {it.toggle !== undefined && <span className="studio-item-switch" aria-hidden="true" />}
                  {it.right && <span className="rt">{it.right}</span>}
                </button>
              )}
            </Fragment>
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}

function Mono({ letter, tone }: { letter: string; tone: number }) {
  return <i className="mi-mono" data-tone={tone} style={{ "--tone": `var(--cat-${tone + 1})` } as React.CSSProperties} aria-hidden="true">{letter}</i>;
}

// Group order is kept rather than ranked by relevance: a list that reorders
// itself as you type moves the row out from under the cursor.
function matching(items: MenuItem[], query: string): MenuItem[] {
  const words = query.split(/\s+/).map(normalize).filter(Boolean);
  if (!words.length) return items;
  // A row is searchable by the heading above it too, so the account's name
  // finds models whose own names never mention it.
  let head = "";
  const hit = items.map((it) => {
    if (it.header) {
      head = normalize(`${it.label} ${it.right ?? ""}`);
      return false;
    }
    // Actions are not data — filtering them out would take "打开其他目录…"
    // away exactly when the query found nothing and the user needs it.
    if (it.plain) return false;
    const hay = `${head}|${normalize(`${it.label} ${it.right ?? ""} ${it.meta ?? ""} ${it.desc ?? ""}`)}`;
    return words.every((w) => hay.includes(w));
  });
  return items.filter((it, i) => {
    if (it.plain) return true;
    if (!it.header) return hit[i];
    for (let j = i + 1; j < items.length && !items[j].header; j++) {
      if (hit[j]) return true;
    }
    return false;
  });
}

// Providers punctuate names inconsistently, so "gpt4" has to find "gpt-4o":
// the separators carry nothing worth matching on.
function normalize(s: string): string {
  return s.toLowerCase().replace(/[\s\-_/.]+/g, "");
}
