import { type ReactNode, createContext, useContext, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { chord } from "./keys";

// The rail is a list of machines, so the box at the top of it asks the whole
// list. It owns the word and publishes it; both lists read it. Lifting the state
// into App would separate the box from who uses it, and leaving it with the
// local half would make the remote half ask a sibling for it.
const RailQuery = createContext("");

/** What the rail is searching for, trimmed and lowercased. Empty when idle. */
export const useRailQuery = () => useContext(RailQuery).trim().toLowerCase();

export function RailSearch({ children }: { children: ReactNode }) {
  const [q, setQ] = useState("");
  const find = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "k" || !(ev.metaKey || ev.ctrlKey)) return;
      ev.preventDefault();
      find.current?.focus();
      find.current?.select();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);

  return (
    <>
      <div className="wsfind">
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M7.2 3.1a4.1 4.1 0 1 0 0 8.2 4.1 4.1 0 0 0 0-8.2M10.3 10.3 13 13" />
        </svg>
        <input
          ref={find}
          type="search"
          value={q}
          onChange={(ev) => setQ(ev.target.value)}
          onKeyDown={(ev) => ev.key === "Escape" && setQ("")}
          placeholder={t("搜索会话 / 项目")}
          aria-label={t("搜索会话 / 项目")}
        />
        <kbd hidden={!!q}>{chord("K")}</kbd>
        <button className="clr" hidden={!q} onClick={() => setQ("")} aria-label={t("清空")}>
          ×
        </button>
      </div>
      <RailQuery.Provider value={q}>{children}</RailQuery.Provider>
    </>
  );
}
