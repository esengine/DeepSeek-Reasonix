import {
  MessageSquare,
  Server,
  Settings,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { t, type Locale, type MessageKey } from "../i18n/messages";

export type Tab = "sessions" | "nodes" | "providers" | "settings";

const TABS: { id: Tab; icon: LucideIcon; label: MessageKey }[] = [
  { id: "sessions", icon: MessageSquare, label: "tab.sessions" },
  { id: "nodes", icon: Server, label: "tab.nodes" },
  { id: "providers", icon: Sparkles, label: "tab.providers" },
  { id: "settings", icon: Settings, label: "tab.settings" },
];

export function TabBar({
  tab,
  locale,
  onChange,
}: {
  tab: Tab;
  locale: Locale;
  onChange: (tab: Tab) => void;
}) {
  return (
    <nav className="tab-bar" aria-label="Primary">
      {TABS.map((item) => {
        const Icon = item.icon;
        const current = tab === item.id;
        return (
          <button
            key={item.id}
            type="button"
            className="tab-item"
            onClick={() => onChange(item.id)}
            aria-current={current ? "page" : undefined}
          >
            <Icon aria-hidden strokeLinecap="round" strokeLinejoin="round" />
            <span>{t(locale, item.label)}</span>
          </button>
        );
      })}
    </nav>
  );
}
