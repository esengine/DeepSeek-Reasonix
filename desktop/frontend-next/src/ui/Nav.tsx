import { t } from "../i18n";

// Drawn on the same 16-unit grid at the same 1.5 stroke as Sym's marks, and for
// the same reason: an icon set pulled in from outside reads as a different hand
// next to the ones the transcript already draws.
const ICON: Record<string, string> = {
  chat: "M3.2 4.4a1.2 1.2 0 0 1 1.2-1.2h7.2a1.2 1.2 0 0 1 1.2 1.2v5a1.2 1.2 0 0 1-1.2 1.2H7.2L4.4 13v-2.4a1.2 1.2 0 0 1-1.2-1.2Z",
  usage: "M3.6 12.4V8.6M7 12.4V4.4M10.4 12.4V7M13 12.4V9.8",
  tools: "M8 2.6 12.5 4.3v4c0 2.6-1.9 4.3-4.5 5-2.6-.7-4.5-2.4-4.5-5v-4ZM6.2 8.1l1.4 1.4 2.4-2.7",
  ext: "M8 2.8 13.2 8 8 13.2 2.8 8Z",
  memory: "M4.6 3h6.8v10L8 10.5 4.6 13Z",
  remote: "M2.9 4.3a1.2 1.2 0 0 1 1.2-1.2h7.8a1.2 1.2 0 0 1 1.2 1.2v5.1a1.2 1.2 0 0 1-1.2 1.2H4.1a1.2 1.2 0 0 1-1.2-1.2ZM6.4 13h3.2M8 10.6V13",
  account: "M8 3.2a2.3 2.3 0 1 0 0 4.6 2.3 2.3 0 0 0 0-4.6M3.9 13v-.9A2.9 2.9 0 0 1 6.8 9.2h2.4a2.9 2.9 0 0 1 2.9 2.9v.9",
  settings: "M3 5h5.2M11.2 5H13M3 11h2.2M8.2 11H13M9.4 3.4v3.2M6.4 9.4v3.2",
};

// Every entry goes somewhere that exists. A rail of icons that open nothing is
// the cheapest thing to draw and the most expensive thing to keep.
const TO: [keyof typeof ICON, string, string | null][] = [
  ["chat", "会话", null],
  ["usage", "用量", "usage"],
  ["tools", "工具与权限", "tools"],
  ["ext", "扩展", "ext"],
  ["memory", "记忆", "memory"],
  ["remote", "远程", "remote"],
];

const FOOT: [keyof typeof ICON, string, string][] = [
  ["account", "账号", "account"],
  ["settings", "设置", ""],
];

interface Props {
  at: string | null;
  onGo: (sec?: string) => void;
  onHome: () => void;
}

export function Nav({ at, onGo, onHome }: Props) {
  const btn = ([key, label, sec]: [keyof typeof ICON, string, string | null]) => (
    <button
      key={key}
      className="navbtn"
      aria-label={t(label)}
      aria-current={(sec ?? null) === at ? "page" : undefined}
      onClick={() => (sec === null ? onHome() : onGo(sec || undefined))}
    >
      <svg viewBox="0 0 16 16" aria-hidden="true">
        <path d={ICON[key]} />
      </svg>
      <span className="navhint" role="tooltip">{t(label)}</span>
    </button>
  );
  return (
    <nav className="nav" aria-label={t("主导航")}>
      <div className="navgrp">{TO.map(btn)}</div>
      <div className="navgrp foot">{FOOT.map(btn)}</div>
    </nav>
  );
}
