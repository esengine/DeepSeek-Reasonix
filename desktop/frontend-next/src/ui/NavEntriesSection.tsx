import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { navLayout, onNavLayoutChange, ordered, resetNavLayout, setNavLayout } from "../state/navEntries";
import { Group } from "./Group";
import { DESTINATIONS } from "./navdest";
import { Switch } from "./Switch";

const NAMES = new Map(DESTINATIONS.map(([mark, label]) => [mark as string, label]));
const KEYS = DESTINATIONS.map(([mark]) => mark as string);

export function NavEntriesSection() {
  const layout = useSyncExternalStore(onNavLayoutChange, navLayout, navLayout);
  const list = ordered(KEYS, layout);
  const hidden = new Set(layout.hidden);
  const toggle = (key: string) => setNavLayout({ order: list, hidden: hidden.has(key) ? layout.hidden.filter((k) => k !== key) : [...layout.hidden, key] });
  const move = (key: string, by: -1 | 1) => {
    const at = list.indexOf(key);
    const next = [...list];
    [next[at], next[at + by]] = [next[at + by]!, next[at]!];
    setNavLayout({ order: next, hidden: layout.hidden });
  };
  const changed = layout.order.length > 0 || layout.hidden.length > 0;
  return (
    <Group id="naventries" title={t("导航入口")} hint={t("图标栏和侧栏里直达各页面的入口：可以隐藏，也可以调整先后。会话、反馈、账号和设置始终保留。")}>
      {list.map((key, i) => (
        <div className="lrow" key={key}>
          <span className="tx">
            <span className="lb">{t(NAMES.get(key)!)}</span>
          </span>
          <button className="btn sm" data-action="appearance.nav-entry-move" data-value={`${key}:up`} disabled={i === 0} aria-label={t("上移：{name}", { name: t(NAMES.get(key)!) })} onClick={() => move(key, -1)}>↑</button>
          <button className="btn sm" data-action="appearance.nav-entry-move" data-value={`${key}:down`} disabled={i === list.length - 1} aria-label={t("下移：{name}", { name: t(NAMES.get(key)!) })} onClick={() => move(key, 1)}>↓</button>
          <Switch data-action="appearance.nav-entry-visible" data-value={key} on={!hidden.has(key)} label={t("显示：{name}", { name: t(NAMES.get(key)!) })} onClick={() => toggle(key)} />
        </div>
      ))}
      <button className="btn sm" data-action="appearance.nav-entries-reset" disabled={!changed} onClick={resetNavLayout}>{t("还原排列")}</button>
    </Group>
  );
}
