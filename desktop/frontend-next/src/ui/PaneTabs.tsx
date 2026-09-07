import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { RuntimeView } from "../port/hub";
import { arrowTabs } from "./tablist";
import { pinToViewport } from "./place";
import { useMarker } from "./marker";

export interface TabView {
  rt: RuntimeView;
  title: string;
  run: string;
  live: boolean;
}

interface Props {
  tabs: TabView[];
  active: string;
  // True when the panes span more than one folder — only then is the folder
  // worth the room, because otherwise every tab repeats the same word.
  showRoot: boolean;
  onFocus: (id: string) => void;
  onClose: (ids: string[]) => void;
  onRename: (rt: RuntimeView, title: string) => void;
}

export function PaneTabs({ tabs, active, showRoot, onFocus, onClose, onRename }: Props) {
  const bar = useRef<HTMLDivElement>(null);
  const mark = useMarker(bar, '.ptab[aria-selected="true"]', "x", [active, tabs.length]);
  // id + 屏幕坐标：页签条是横向滚动容器，overflow 会把 absolute 定位的菜单
  // 裁掉（点了像没反应），所以菜单挂在 fixed 上，位置由触发点决定。
  const [menu, setMenu] = useState<{ id: string; x: number; y: number } | null>(null);
  const [editing, setEditing] = useState("");
  // Which close is waiting on an answer, and what it would take with it.
  const [confirm, setConfirm] = useState<{ ids: string[]; live: string[] } | null>(null);

  // A tab scrolled out of the strip is a tab you cannot see you are on. On the
  // next frame: this fires exactly when a second pane opens, which is also when
  // a whole transcript is mounting, and scrollIntoView inside that commit pays
  // for a layout of all of it.
  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      bar.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
    return () => cancelAnimationFrame(raf);
  }, [active, tabs.length]);

  // A trackpad only ever sends deltaY here, so without this the strip scrolls
  // the page instead of itself.
  useEffect(() => {
    const el = bar.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (e.deltaX !== 0 || el.scrollWidth <= el.clientWidth) return;
      e.preventDefault();
      el.scrollLeft += e.deltaY;
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  useEffect(() => {
    if (!menu) return;
    const shut = () => setMenu(null);
    addEventListener("click", shut);
    addEventListener("keydown", shut);
    return () => {
      removeEventListener("click", shut);
      removeEventListener("keydown", shut);
    };
  }, [menu]);

  // Closing what is still working needs an answer first; closing idle panes is
  // just tidying and happens straight away.
  const close = (ids: string[]) => {
    const live = ids.filter((id) => tabs.find((t) => t.rt.id === id)?.live);
    if (live.length === 0) {
      onClose(ids);
      return;
    }
    setConfirm({ ids, live });
  };

  const others = (id: string) => tabs.filter((t) => t.rt.id !== id).map((t) => t.rt.id);

  if (confirm) {
    const names = confirm.live
      .map((id) => tabs.find((t) => t.rt.id === id)?.title ?? id)
      .slice(0, 3)
      .join("、");
    return (
      <div className="panetabs">
        <div className="tabconfirm" role="alertdialog">
          <span className="q">
            {confirm.ids.length > 1 ? t("关闭 {n} 个面板？", { n: confirm.ids.length }) : t("关闭这个面板？")}
          </span>
          <span className="h">
            {confirm.live.length === 1 ? t("{names} 还在跑", { names }) : t("其中 {n} 个还在跑：{names}", { n: confirm.live.length, names })}
            {t("，关掉会停下它")}
          </span>
          <div className="a">
            <button onClick={() => setConfirm(null)}>{t("取消")}</button>
            <button
              data-danger=""
              data-action="pane.close"
              autoFocus
              onClick={() => {
                onClose(confirm.ids);
                setConfirm(null);
              }}
            >
              {t("关闭")}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="panetabs" ref={bar} role="tablist" aria-label={t("会话面板")} onKeyDown={arrowTabs}>
      {tabs.map(({ rt, title, run }) => (
        <div
          key={rt.id}
          className="ptab"
          data-action-click="pane.activate"
          role="tab"
          tabIndex={rt.id === active ? 0 : -1}
          aria-selected={rt.id === active}
          data-run={run}
          title={rt.root}
          onClick={() => onFocus(rt.id)}
          onDoubleClick={() => setEditing(rt.id)}
          onContextMenu={(ev) => {
            // 这个窗口的复制粘贴只有系统右键菜单一个来源（Wails 的
            // EnableDefaultContextMenu 就是为此开的），所以输入框里和有选区时
            // 一律让位 —— 自定义菜单只是快捷方式，不该抢走唯一的那条路。
            const el = ev.target as HTMLElement;
            if (el.closest("input, textarea") || document.getSelection()?.isCollapsed === false) return;
            ev.preventDefault();
            setMenu({ id: rt.id, x: ev.clientX, y: ev.clientY });
          }}
        >
          <i className="pip" />
          {editing === rt.id ? (
            <input
              className="ptab-in"
              autoFocus
              defaultValue={title}
              onClick={(ev) => ev.stopPropagation()}
              onBlur={(ev) => {
                setEditing("");
                if (ev.currentTarget.value.trim() !== title) onRename(rt, ev.currentTarget.value.trim());
              }}
              onKeyDown={(ev) => {
                if (ev.key === "Enter") ev.currentTarget.blur();
                if (ev.key === "Escape") {
                  // Abandoning a rename is not stopping the run behind it.
                  ev.stopPropagation();
                  ev.currentTarget.value = title;
                  ev.currentTarget.blur();
                }
              }}
            />
          ) : (
            <span className="ptab-nm">{title}</span>
          )}
          {showRoot && <span className="ptab-ws">{rt.name}</span>}
          {/* Which machine is running this. Panes from two hosts sit side by
              side, and a command lands wherever the focused tab points. */}
          {rt.host && <span className="ptab-host">{rt.host}</span>}
          <button
            className="ptab-x"
            data-action="pane.close"
            data-value="one"
            title={t("关闭这个面板")}
            aria-label={t("关闭这个面板")}
            onClick={(ev) => {
              ev.stopPropagation();
              close([rt.id]);
            }}
          >
            ×
          </button>

        </div>
      ))}

      {mark && <i className="tabmark" style={{ width: mark.len, transform: `translateX(${mark.at}px)` }} />}

      {menu && (
        <div
          className="tabmenu"
          role="menu"
          ref={(el) => {
            if (!el) return;
            // This used to clamp against innerWidth - 152, where 152 was the menu
            // width: a CSS fact copied into JS that drifts as labels grow, and it
            // only clamped one edge. pinToViewport measures the menu and clamps
            // both, in the one place that knows about the zoom.
            pinToViewport(el, menu.x, menu.y);
          }}
          onClick={(ev) => ev.stopPropagation()}
        >
          <button role="menuitem" onClick={() => { const id = menu.id; setMenu(null); setEditing(id); }}>
            {t("重命名")}
          </button>
          <button role="menuitem" data-action="pane.close" data-value="one" onClick={() => { const id = menu.id; setMenu(null); close([id]); }}>
            {t("关闭")}
          </button>
          <button
            data-action="pane.close"
            data-value="others"
            role="menuitem"
            disabled={tabs.length < 2}
            onClick={() => { const id = menu.id; setMenu(null); close(others(id)); }}
          >
            关闭其他（{Math.max(tabs.length - 1, 0)}）
          </button>
          <button role="menuitem" data-action="pane.close" data-value="all" onClick={() => { setMenu(null); close(tabs.map((t) => t.rt.id)); }}>
            全部关闭（{tabs.length}）
          </button>
        </div>
      )}
    </div>
  );
}
