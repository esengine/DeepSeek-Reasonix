import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { RuntimeView } from "../port/hub";
import { arrowTabs } from "./tablist";
import { pinToViewport } from "./place";
import { useMarker } from "./marker";
import { useDismiss } from "./dismiss";

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
  // Enter commits and then blurs, and both would otherwise send the same name.
  const renamed = useRef<Record<string, string>>({});
  const rename = (rt: RuntimeView, was: string, raw: string) => {
    const next = raw.trim();
    if (!next || next === was || renamed.current[rt.id] === next) return;
    renamed.current[rt.id] = next;
    onRename(rt, next);
  };
  // Which close is waiting on an answer. The ids only: which of them still
  // exist and which are still running are read off the current tabs on every
  // render and again when the answer comes, because a set captured when the
  // question was asked stops being true while it is on screen — a pane can
  // finish, or be closed from somewhere else, in the seconds it takes to read.
  const [confirm, setConfirm] = useState<{ ids: string[]; kind: string; from: string } | null>(null);
  const box = useRef<HTMLDivElement>(null);

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

  // What the question is about, now: ids whose tab is still on the strip, and
  // of those the ones still working. Derived rather than stored — see above.
  const present = confirm ? confirm.ids.filter((id) => tabs.some((t) => t.rt.id === id)) : [];
  const liveNow = present.filter((id) => tabs.find((t) => t.rt.id === id)?.live);
  const nameOf = (id: string) => tabs.find((t) => t.rt.id === id)?.title ?? id;

  // Back to the control that asked, which is the close button on the tab this
  // is about — looked up now rather than held since it was pressed, because
  // the pane it sits on may have gone in the meantime. A menu item cannot be
  // returned to at all: choosing it is what closed the menu.
  const cancel = useCallback(() => {
    const at = confirm?.from;
    setConfirm(null);
    const back = at ? bar.current?.querySelector<HTMLElement>(`[data-pane="${CSS.escape(at)}"] .ptab-x`) : null;
    (back ?? bar.current?.querySelector<HTMLElement>('[aria-selected="true"]'))?.focus();
  }, [confirm?.from]);

  useDismiss(!!confirm, box, cancel);

  // Nothing left to close: the question answered itself elsewhere, and leaving
  // it up would offer to stop panes that are already gone.
  useEffect(() => {
    if (confirm && present.length === 0) cancel();
    // The sources, not the projection of them: depending on present.length
    // declares an edge to a value derived here, which nothing outside this
    // render can trace back — and the census loses the whole cell's provenance
    // with it, taking five unrelated handlers in this file from read-only to
    // "might write".
  }, [confirm, tabs, cancel]);

  // Closing what is still working needs an answer first; closing idle panes is
  // just tidying and happens straight away.
  const close = (ids: string[], kind: string, from: string) => {
    if (!ids.some((id) => tabs.find((t) => t.rt.id === id)?.live)) {
      onClose(ids);
      return;
    }
    setConfirm({ ids, kind, from });
  };

  // The answer acts on what is live at this moment, not on what was live when
  // the question went up. An id whose pane has since gone is not sent back to
  // the host, and a pane that has since finished is closed without the claim
  // that closing stops it.
  const commit = () => {
    const ids = confirm ? confirm.ids.filter((id) => tabs.some((t) => t.rt.id === id)) : [];
    setConfirm(null);
    if (ids.length > 0) onClose(ids);
  };

  const others = (id: string) => tabs.filter((t) => t.rt.id !== id).map((t) => t.rt.id);

  return (
    <div className="panetabs" ref={bar} role="tablist" aria-label={t("会话面板")} onKeyDown={arrowTabs}>
      {tabs.map(({ rt, title, run }) => (
        <div
          key={rt.id}
          className="ptab"
          data-pane={rt.id}
          data-action-click="pane.activate"
          role="tab"
          tabIndex={rt.id === active ? 0 : -1}
          aria-selected={rt.id === active}
          data-run={run}
          title={rt.root}
          onClick={() => onFocus(rt.id)}
          onDoubleClick={() => setEditing(rt.id)}
          onContextMenu={(ev) => {
            // The system context menu is this window's only copy/paste route,
            // so a field or a live selection yields to it: the custom menu is a
            // shortcut and must not take the only path away.
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
                rename(rt, title, ev.currentTarget.value);
              }}
              data-action-keydown="session.rename"
              data-target={rt.id}
              onKeyDown={(ev) => {
                if (ev.key === "Enter") {
                  // The aimed-at commit; the blur it causes is guarded above.
                  rename(rt, title, ev.currentTarget.value);
                  ev.currentTarget.blur();
                }
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
              close([rt.id], "one", rt.id);
            }}
          >
            ×
          </button>

        </div>
      ))}

      {mark && <i className="tabmark" style={{ width: mark.len, transform: `translateX(${mark.at}px)` }} />}

      {confirm && present.length > 0 && (
        <div
          className="tabconfirm"
          role="alertdialog"
          // Named by the question it is asking, so the accessible name and
          // what is on screen cannot drift apart.
          aria-labelledby="tabconfirm-q"
          ref={(el) => {
            // The same node the dismiss watches: without it every press inside
            // the question reads as a press away, and answering cancels first.
            box.current = el;
            if (!el) return;
            // Anchored under the tab this is about, through the same viewport
            // seam the context menu uses — a bubble near the right edge is
            // moved rather than clipped, and the zoom is corrected in one place.
            const tab = bar.current?.querySelector<HTMLElement>(`[data-pane="${CSS.escape(confirm.from)}"]`);
            const at = (tab ?? bar.current)?.getBoundingClientRect();
            if (at) pinToViewport(el, at.left, at.bottom + 6);
          }}
        >
          <span className="q" id="tabconfirm-q">
            {present.length > 1 ? t("关闭 {n} 个面板？", { n: present.length }) : t("关闭这个面板？")}
          </span>
          <span className="h">
            {liveNow.length === 0
              ? t("均已停止")
              : (liveNow.length === 1
                  ? t("{names} 仍在运行", { names: nameOf(liveNow[0]) })
                  : t("其中 {n} 个仍在运行：{names}", { n: liveNow.length, names: liveNow.slice(0, 3).map(nameOf).join("、") })) +
                t("，关闭将使其停止")}
          </span>
          <div className="a">
            <button onClick={cancel}>{t("取消")}</button>
            <button data-danger="" data-action="pane.close" data-value={confirm.kind} autoFocus onClick={commit}>
              {t("关闭")}
            </button>
          </div>
        </div>
      )}

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
          <button role="menuitem" data-action="pane.close" data-value="one" onClick={() => { const id = menu.id; setMenu(null); close([id], "one", id); }}>
            {t("关闭")}
          </button>
          <button
            data-action="pane.close"
            data-value="others"
            role="menuitem"
            disabled={tabs.length < 2}
            onClick={() => { const id = menu.id; setMenu(null); close(others(id), "others", id); }}
          >
            {t("关闭其他（{n}）", { n: Math.max(tabs.length - 1, 0) })}
          </button>
          <button role="menuitem" data-action="pane.close" data-value="all" onClick={() => { const id = menu.id; setMenu(null); close(tabs.map((t) => t.rt.id), "all", id); }}>
            {t("全部关闭（{n}）", { n: tabs.length })}
          </button>
        </div>
      )}
    </div>
  );
}
