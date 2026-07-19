"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";

import { searchIndex } from "@/app/content";

export function SearchPanel() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen(true);
      }
      if (event.key === "Escape") setOpen(false);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    if (open) window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open]);

  const results = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return searchIndex.slice(0, 7);

    return searchIndex
      .filter((record) =>
        `${record.title} ${record.description} ${record.section} ${record.keywords}`
          .toLowerCase()
          .includes(normalized),
      )
      .slice(0, 9);
  }, [query]);

  return (
    <>
      <button className="search-trigger" type="button" onClick={() => setOpen(true)}>
        <span aria-hidden="true">⌕</span>
        <span>搜索开发地图</span>
        <kbd>⌘ K</kbd>
      </button>

      {open ? (
        <div className="search-backdrop" role="presentation" onMouseDown={() => setOpen(false)}>
          <section
            aria-label="站内搜索"
            aria-modal="true"
            className="search-dialog"
            role="dialog"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="search-input-wrap">
              <span aria-hidden="true">⌕</span>
              <input
                ref={inputRef}
                aria-label="搜索架构、模块或开发任务"
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索架构、模块或开发任务…"
                value={query}
              />
              <button type="button" onClick={() => setOpen(false)} aria-label="关闭搜索">
                ESC
              </button>
            </div>

            <div className="search-results" aria-live="polite">
              {results.length ? (
                results.map((record) => (
                  <Link key={`${record.href}-${record.title}`} href={record.href} onClick={() => setOpen(false)}>
                    <span className="search-result-section">{record.section}</span>
                    <strong>{record.title}</strong>
                    <span>{record.description}</span>
                    <i aria-hidden="true">↗</i>
                  </Link>
                ))
              ) : (
                <p className="search-empty">没有直接匹配。试试 “会话”、 “Wails” 或 “Provider”。</p>
              )}
            </div>
          </section>
        </div>
      ) : null}
    </>
  );
}
