import { useCallback, useEffect, useRef, useState, type MouseEvent, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { BrowserTabMenu } from "./BrowserTabMenu";
import { keyOf, labelOf, type Surface } from "./workbench_tabs";

export function WorkbenchTabs({ surfaces, hosts, active, showFiles, closing, onPick, onClose, onFiles, onNew }: {
  surfaces: Surface[];
  hosts: Record<string, string>;
  active: string;
  showFiles: boolean;
  closing: boolean;
  onPick: (key: string) => void;
  onClose: (targets: Surface[]) => Promise<void>;
  onFiles: () => void;
  onNew: () => void;
}) {
  const strip = useRef<HTMLDivElement>(null);
  const [menu, setMenu] = useState<{ surface: Surface; x: number; y: number; anchor: HTMLElement } | null>(null);
  const returning = useRef("");
  const at = menu ? surfaces.findIndex((surface) => keyOf(surface) === keyOf(menu.surface)) : -1;
  const others = surfaces.filter((surface) => surface.kind !== "file" && keyOf(surface) !== (menu && keyOf(menu.surface)));
  const right = at < 0 ? [] : surfaces.slice(at + 1).filter((surface) => surface.kind !== "file");
  const dismiss = useCallback(() => setMenu(null), []);
  useEffect(() => {
    strip.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [active, surfaces.length]);
  useEffect(() => {
    if (menu && at < 0) dismiss();
    if (menu || closing || !returning.current) return;
    const buttons = [...(strip.current?.querySelectorAll<HTMLButtonElement>('[data-action-click="workbench.tab"]') ?? [])];
    const back = buttons.find((button) => button.dataset.target === returning.current) ?? buttons.find((button) => button.closest('[aria-selected="true"]'));
    returning.current = "";
    (back ?? strip.current?.parentElement?.querySelector<HTMLButtonElement>(".workbench-files"))?.focus();
  }, [menu, at, surfaces, closing, dismiss]);
  const offer = (event: MouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>, surface: Surface) => {
    if (surface.kind === "file" || closing || document.getSelection()?.isCollapsed === false) return;
    event.preventDefault();
    event.stopPropagation();
    const anchor = event.currentTarget.matches("button") ? event.currentTarget : event.currentTarget.querySelector<HTMLElement>(".workbench-tab-pick")!;
    const rect = anchor.getBoundingClientRect();
    const pointed = "clientX" in event && (event.clientX !== 0 || event.clientY !== 0);
    setMenu({ surface, x: pointed ? event.clientX : rect.left, y: pointed ? event.clientY : rect.bottom, anchor });
  };
  return (
    <header className="workbench-tabs">
      <div className="workbench-tablist" role="tablist" ref={strip}
        onWheel={(event) => { if (event.deltaY && strip.current) strip.current.scrollLeft += event.deltaY; }}>
        {surfaces.map((surface) => {
          const key = keyOf(surface), label = labelOf(surface, hosts), browser = surface.kind !== "file";
          return (
            <div className="workbench-tab" key={key} role="tab" aria-selected={key === active}
              data-live={surface.kind === "browser" && surface.tab.active ? "" : undefined}
              data-action-contextmenu={browser ? "workbench.browser-menu" : undefined} data-target={key}
              onContextMenu={(event) => offer(event, surface)}>
              <button className="workbench-tab-pick" data-action-click="workbench.tab" data-target={key}
                title={surface.kind === "browser" ? `${surface.tab.active ? `${t("模型正在操作这个页面")}\n` : ""}${surface.tab.url}` : label}
                data-action-keydown={browser ? "workbench.browser-menu" : undefined}
                aria-haspopup={browser ? "menu" : undefined} aria-expanded={browser ? !!menu && keyOf(menu.surface) === key : undefined}
                onClick={() => onPick(key)} onKeyDown={(event) => {
                  if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) offer(event, surface);
                }}>
                <StudioIcon name={surface.kind === "file" ? "file" : "globe"} /><span>{label}</span>
              </button>
              <button className="workbench-tab-close" data-action="workbench.close" data-target={key}
                aria-label={t("关闭 {name}", { name: label })} title={t("关闭")} disabled={closing} onClick={() => void onClose([surface])}>
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
      {menu && at >= 0 && <BrowserTabMenu menu={menu} others={others.length > 0} right={right.length > 0} onDismiss={dismiss} onClose={(mode) => {
        returning.current = keyOf(menu.surface);
        dismiss();
        void onClose(mode === "one" ? [menu.surface] : mode === "others" ? others : mode === "right" ? right : surfaces.filter((surface) => surface.kind !== "file"));
      }} />}
    </header>
  );
}
