import { useEffect, useRef, useState, type MouseEvent, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { pinToViewport } from "./place";
import { useDismiss } from "./dismiss";
import { workspaceMenuKeys } from "./WorkspaceOrder";

export function WorkbenchTabs({ items, active, showFiles, onPick, onClose, onFiles, onNew }: {
  items: { key: string; kind: "manual" | "browser" | "file"; label: string; title: string; live: boolean }[];
  active: string;
  showFiles: boolean;
  onPick: (key: string) => void;
  onClose: (keys: string[]) => void;
  onFiles: () => void;
  onNew: () => void;
}) {
  const strip = useRef<HTMLDivElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const [menu, setMenu] = useState<{ key: string; x: number; y: number } | null>(null);
  const returning = useRef("");
  const at = menu ? items.findIndex((item) => item.key === menu.key) : -1;
  const others = items.filter((item) => item.kind !== "file" && item.key !== menu?.key).map((item) => item.key);
  const right = at < 0 ? [] : items.slice(at + 1).filter((item) => item.kind !== "file").map((item) => item.key);
  const dismiss = () => { returning.current = menu?.key ?? ""; setMenu(null); };
  useDismiss(!!menu, box, dismiss);
  useEffect(() => {
    strip.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [active, items.length]);
  useEffect(() => {
    if (menu && at < 0) dismiss();
    if (menu || !returning.current) return;
    const buttons = [...(strip.current?.querySelectorAll<HTMLButtonElement>('[data-action-click="workbench.tab"]') ?? [])];
    const back = buttons.find((button) => button.dataset.target === returning.current) ?? buttons.find((button) => button.dataset.target === active);
    returning.current = "";
    (back ?? strip.current?.parentElement?.querySelector<HTMLButtonElement>(".workbench-files"))?.focus();
  }, [menu, items, active]);
  useEffect(() => {
    if (menu) box.current?.querySelector<HTMLButtonElement>('button[role="menuitem"]')?.focus();
  }, [menu]);
  const offer = (event: MouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>, key: string) => {
    if (document.getSelection()?.isCollapsed === false) return;
    event.preventDefault();
    event.stopPropagation();
    const row = event.currentTarget.getBoundingClientRect();
    const pointed = "clientX" in event && (event.clientX !== 0 || event.clientY !== 0);
    setMenu({ key, x: pointed ? event.clientX : row.left, y: pointed ? event.clientY : row.bottom });
  };
  const close = (keys: string[]) => { dismiss(); onClose(keys); };
  return (
    <header className="workbench-tabs">
      <div className="workbench-tablist" role="tablist" ref={strip}
        onWheel={(event) => { if (event.deltaY && strip.current) strip.current.scrollLeft += event.deltaY; }}>
        {items.map((item) => {
          const browser = item.kind !== "file";
          return (
          <div className="workbench-tab" key={item.key} role="tab" aria-selected={item.key === active} data-live={item.live ? "" : undefined}>
            <button className="workbench-tab-pick" data-action-click="workbench.tab" data-target={item.key} title={item.title}
              data-action-contextmenu={browser ? "workbench.tab-menu" : undefined}
              data-action-keydown={browser ? "workbench.tab-menu" : undefined}
              aria-haspopup={browser ? "menu" : undefined} aria-expanded={browser ? menu?.key === item.key : undefined}
              onClick={() => onPick(item.key)}
              onContextMenu={(event) => { if (browser) offer(event, item.key); }}
              onKeyDown={(event) => { if (browser && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) offer(event, item.key); }}>
              <StudioIcon name={item.kind === "file" ? "file" : "globe"} /><span>{item.label}</span>
            </button>
            <button className="workbench-tab-close" data-action="workbench.close" data-target={item.key}
              aria-label={t("关闭 {name}", { name: item.label })} title={t("关闭")} onClick={() => onClose([item.key])}>
              <StudioIcon name="close" />
            </button>
          </div>
          );
        })}
      </div>
      <button className="workbench-files" data-action="workbench.files" aria-pressed={showFiles} aria-label={t("文件")} title={t("文件")} onClick={onFiles}>
        <StudioIcon name="folder" />
      </button>
      <button className="workbench-new" data-action="workbench.new-browser" aria-label={t("新建浏览器标签")} title={t("新建浏览器标签")} onClick={onNew}>
        <StudioIcon name="plus" />
      </button>
      {menu && at >= 0 && createPortal(
        <div className="tabmenu" role="menu" aria-label={t("浏览器标签操作")} data-action-keydown="workbench.tab-menu" data-action-click="workbench.tab-menu" data-target={menu.key}
          ref={(element) => { box.current = element; if (element) pinToViewport(element, menu.x, menu.y); }}
          onKeyDown={workspaceMenuKeys} onClick={(event) => event.stopPropagation()}>
          <button role="menuitem" data-action="workbench.close" data-target={menu.key} data-value="one" onClick={() => close([menu.key])}>{t("关闭这个标签")}</button>
          <button role="menuitem" data-action="workbench.close" data-target={menu.key} data-value="others" disabled={others.length === 0} onClick={() => close(others)}>{t("关闭其他浏览器标签")}</button>
          <button role="menuitem" data-action="workbench.close" data-target={menu.key} data-value="right" disabled={right.length === 0} onClick={() => close(right)}>{t("关闭右侧浏览器标签")}</button>
        </div>, document.body,
      )}
    </header>
  );
}
