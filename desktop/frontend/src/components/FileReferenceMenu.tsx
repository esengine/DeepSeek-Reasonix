import { useEffect, useMemo, useRef, useState } from "react";
import { FileText, Folder } from "lucide-react";
import { asArray } from "../lib/array";
import { filterAtMatches } from "../lib/atMatches";
import { app } from "../lib/bridge";
import type { DirEntry } from "../lib/types";
import { VirtualMenu } from "./VirtualMenu";

export function dirEntrySubmitPath(entry: DirEntry, atDir: string): string {
  return entry.path || atDir + entry.name;
}

export function dirEntryMenuLabel(entry: DirEntry): string {
  return entry.displayName || entry.name;
}

export function activeFileReferenceToken(text: string): { raw: string; dir: string; frag: string } | null {
  const match = /(?:^|\s)@([^\s]*)$/.exec(text);
  if (!match) return null;
  const raw = match[1];
  const slash = raw.lastIndexOf("/");
  return {
    raw,
    dir: slash >= 0 ? raw.slice(0, slash + 1) : "",
    frag: (slash >= 0 ? raw.slice(slash + 1) : raw).toLowerCase(),
  };
}

export function pickInlineFileReference(text: string, atRaw: string | null, atDir: string, entry: DirEntry): string {
  const atPos = text.length - (atRaw?.length ?? 0) - 1;
  const prefix = text.slice(0, Math.max(0, atPos));
  const refPath = dirEntrySubmitPath(entry, atDir);
  return prefix + "@" + refPath + (entry.isDir ? "/" : " ");
}

export function insertTextAtSelection(
  value: string,
  insert: string,
  selectionStart = value.length,
  selectionEnd = selectionStart,
): { value: string; caret: number } {
  const before = value.slice(0, selectionStart);
  const after = value.slice(selectionEnd);
  const needsLeadingSpace = before.length > 0 && !/\s$/.test(before) && !/^\s/.test(insert);
  const needsTrailingSpace = after.length > 0 && !/\s$/.test(insert) && !/^\s/.test(after);
  const text = `${needsLeadingSpace ? " " : ""}${insert}${needsTrailingSpace ? " " : ""}`;
  const next = before + text + after;
  return { value: next, caret: before.length + text.length };
}

export function useFileReferenceMenu(text: string, cwd?: string) {
  const token = useMemo(() => activeFileReferenceToken(text), [text]);
  const atRaw = token?.raw ?? null;
  const atDir = token?.dir ?? "";
  const atFrag = token?.frag ?? "";
  const [entries, setEntries] = useState<DirEntry[]>([]);
  const [searchEntries, setSearchEntries] = useState<DirEntry[]>([]);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const dirCache = useRef<Record<string, DirEntry[]>>({});
  const searchCache = useRef<Record<string, DirEntry[]>>({});
  const prevCwdRef = useRef(cwd);

  useEffect(() => {
    if (prevCwdRef.current === cwd) return;
    prevCwdRef.current = cwd;
    dirCache.current = {};
    searchCache.current = {};
    setEntries([]);
    setSearchEntries([]);
    setActive(0);
    setDismissed(false);
  }, [cwd]);

  useEffect(() => {
    setActive(0);
    setDismissed(false);
  }, [atRaw]);

  useEffect(() => {
    if (atRaw === null) return;
    const cached = dirCache.current[atDir];
    if (cached) {
      setEntries(cached);
      return;
    }
    let live = true;
    app
      .ListDir(atDir)
      .then((next) => {
        const list = asArray(next);
        dirCache.current[atDir] = list;
        if (live) setEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [atRaw === null, atDir, cwd]);

  useEffect(() => {
    if (atRaw === null || atDir !== "" || atFrag === "") {
      setSearchEntries([]);
      return;
    }
    const cached = searchCache.current[atFrag];
    if (cached) {
      setSearchEntries(cached);
      return;
    }
    setSearchEntries([]);
    let live = true;
    app
      .SearchFileRefs(atFrag)
      .then((next) => {
        const list = next ?? [];
        searchCache.current[atFrag] = list;
        if (live) setSearchEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [atRaw === null, atDir, atFrag, cwd]);

  const items = useMemo(() => {
    if (atRaw === null) return [];
    return filterAtMatches(entries, searchEntries, atFrag);
  }, [atRaw, atFrag, entries, searchEntries]);

  useEffect(() => {
    const maxIdx = Math.max(0, items.length - 1);
    setActive((prev) => (prev > maxIdx ? 0 : prev));
  }, [items.length]);

  return {
    atRaw,
    atDir,
    items,
    active,
    setActive,
    count: atRaw !== null && !dismissed ? items.length : 0,
    open: atRaw !== null && !dismissed,
    dismiss: () => setDismissed(true),
  };
}

export function FileReferenceMenu({
  items,
  activeIndex,
  onPick,
  onHover,
}: {
  items: DirEntry[];
  activeIndex: number;
  onPick: (entry: DirEntry) => void;
  onHover: (index: number) => void;
}) {
  const renderEntry = (entry: DirEntry, index: number) => (
    <button
      role="option"
      aria-selected={index === activeIndex}
      className={`slashmenu__item ${index === activeIndex ? "slashmenu__item--active" : ""}`}
      onMouseDown={(event) => {
        event.preventDefault();
        onPick(entry);
      }}
      onMouseMove={() => onHover(index)}
    >
      {entry.isDir ? (
        <Folder size={13} className="filemenu__icon filemenu__icon--dir" />
      ) : (
        <FileText size={13} className="filemenu__icon" />
      )}
      <span className="slashmenu__name slashmenu__name--file">
        {dirEntryMenuLabel(entry)}
        {entry.isDir ? "/" : ""}
      </span>
    </button>
  );

  if (typeof ResizeObserver === "undefined") {
    return (
      <div className="slashmenu" role="listbox">
        {items.map((entry, index) => (
          <div key={(entry.isDir ? "d:" : "f:") + (entry.path || entry.name)}>
            {renderEntry(entry, index)}
          </div>
        ))}
      </div>
    );
  }

  return (
    <VirtualMenu
      items={items}
      activeIndex={activeIndex}
      itemKey={(entry) => (entry.isDir ? "d:" : "f:") + (entry.path || entry.name)}
      renderItem={renderEntry}
    />
  );
}
