import { isAbsolute, parse, relative, resolve } from "node:path";
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import {
  type DirEntry,
  type FileWithStats,
  type ParsedAtQuery,
  detectAtPicker,
  listDirectory,
  parseAtQuery,
  rankPickerCandidates,
  walkFilesStream,
} from "../../at-mentions.js";
import { SkillStore } from "../../skills.js";
import {
  type McpServerSummary,
  type SlashArgContext,
  type SlashCommandSpec,
  countAdvancedCommands,
  detectSlashArgContext,
  suggestSlashCommands,
} from "./slash.js";

export interface UseCompletionPickersParams {
  input: string;
  setInput: (v: string) => void;
  codeMode: { rootDir: string } | undefined;
  /** May differ from `codeMode.rootDir` after `/cwd` — drives file listing, not the mode check. */
  rootDir: string;
  models: string[] | null;
  mcpServers: McpServerSummary[] | undefined;
  /** Cross-session slash invocation counts — used to sort suggestions by frequency. */
  slashUsage?: Readonly<Record<string, number>>;
}

export interface AtPickerEntry {
  /** Basename — what the row leads with. */
  label: string;
  /** Path the picker substitutes into the buffer (no leading @). */
  insertPath: string;
  /** Dim suffix shown after the label ("src/auth/" for "src/auth/login.ts" search hits). Empty in browse mode. */
  dirSuffix: string;
  isDir: boolean;
}

export type AtPickerState =
  | { kind: "browse"; baseDir: string; entries: readonly AtPickerEntry[]; loading: boolean }
  | {
      kind: "search";
      filter: string;
      entries: readonly AtPickerEntry[];
      scanned: number;
      searching: boolean;
    };

export interface UseCompletionPickersResult {
  // ── slash-name picker ──
  slashMatches: SlashCommandSpec[] | null;
  slashSelected: number;
  setSlashSelected: React.Dispatch<React.SetStateAction<number>>;
  /** True when the input is exactly `/` — palette renders group headers. */
  slashGroupMode: boolean;
  /** Count of advanced commands hidden behind the "type to search" footer hint. */
  slashAdvancedHidden: number;

  // ── @-mention picker ──
  atState: AtPickerState | null;
  atSelected: number;
  setAtSelected: React.Dispatch<React.SetStateAction<number>>;
  pickAtMention: (entry: AtPickerEntry, action: "commit" | "drill") => void;
  recordRecentFile: (path: string) => void;

  // ── slash-arg picker ──
  slashArgContext: SlashArgContext | null;
  slashArgMatches: readonly string[] | null;
  slashArgSelected: number;
  setSlashArgSelected: React.Dispatch<React.SetStateAction<number>>;
  /** When the completer is `"path"`, carries the rich entries (with `isDir`) so
   *  callers can distinguish directories from files for drill-down vs commit. */
  slashArgPathCandidates: readonly AtPickerEntry[] | null;
  /** `isDir` controls drill vs. commit for path completers; ignored for other types. */
  pickSlashArg: (chosen: string, isDir?: boolean) => void;
}

const SEARCH_DEBOUNCE_MS = 80;
const SEARCH_FLUSH_MS = 50;
const SEARCH_RESULT_CAP = 200;

/** Picker priority: @ > slash-arg > slash-name. Detection already disambiguates by buffer shape. */
export function useCompletionPickers({
  input,
  setInput,
  codeMode,
  rootDir,
  models,
  mcpServers,
  slashUsage,
}: UseCompletionPickersParams): UseCompletionPickersResult {
  // ── slash-name picker ──
  const [slashSelected, setSlashSelected] = useState(0);
  const slashMatches = useMemo(() => {
    if (!input.startsWith("/") || input.includes(" ")) return null;
    return suggestSlashCommands(input.slice(1), !!codeMode, slashUsage);
  }, [input, codeMode, slashUsage]);
  const slashGroupMode = input === "/";
  const slashAdvancedHidden = useMemo(
    () => (slashGroupMode ? countAdvancedCommands(!!codeMode) : 0),
    [slashGroupMode, codeMode],
  );
  useEffect(() => {
    setSlashSelected((prev) => {
      if (!slashMatches || slashMatches.length === 0) return 0;
      if (prev >= slashMatches.length) return slashMatches.length - 1;
      return prev;
    });
  }, [slashMatches]);

  // ── @-mention picker ──
  const [atSelected, setAtSelected] = useState(0);
  const recentFilesRef = useRef<string[]>([]);
  const recordRecentFile = useCallback((p: string) => {
    const list = recentFilesRef.current;
    const i = list.indexOf(p);
    if (i >= 0) list.splice(i, 1);
    list.unshift(p);
    if (list.length > 20) list.length = 20;
  }, []);

  const atPicker = useMemo(() => {
    if (!codeMode) return null;
    if (slashMatches !== null) return null;
    return detectAtPicker(input);
  }, [codeMode, input, slashMatches]);

  const parsed = useMemo<ParsedAtQuery | null>(
    () => (atPicker ? parseAtQuery(atPicker.query) : null),
    [atPicker],
  );

  const atMode: "browse" | "search" | null = parsed
    ? parsed.trailingSlash || parsed.filter === ""
      ? "browse"
      : "search"
    : null;

  const browseDir = parsed && atMode === "browse" ? parsed.dir : "";
  const browse = useBrowseListing(rootDir, atMode === "browse" ? browseDir : null);
  const search = useStreamingSearch(
    rootDir,
    atMode === "search" && parsed ? parsed.filter : null,
    recentFilesRef,
  );

  const atState = useMemo<AtPickerState | null>(() => {
    if (!parsed) return null;
    if (atMode === "browse") {
      return {
        kind: "browse",
        baseDir: browseDir,
        entries: browse.entries,
        loading: browse.loading,
      };
    }
    return {
      kind: "search",
      filter: parsed.filter,
      entries: search.entries,
      scanned: search.scanned,
      searching: search.searching,
    };
  }, [parsed, atMode, browseDir, browse, search]);

  useEffect(() => {
    setAtSelected((prev) => {
      const len = atState?.entries.length ?? 0;
      if (len === 0) return 0;
      if (prev >= len) return len - 1;
      return prev;
    });
  }, [atState]);

  const pickAtMention = useCallback(
    (entry: AtPickerEntry, action: "commit" | "drill") => {
      if (!atPicker) return;
      const before = input.slice(0, atPicker.atOffset);
      const tail =
        action === "drill" && entry.isDir ? `${entry.insertPath}/` : `${entry.insertPath} `;
      setInput(`${before}@${tail}`);
    },
    [atPicker, input, setInput],
  );

  // ── slash-arg picker ──
  const [slashArgSelected, setSlashArgSelected] = useState(0);
  const slashArgContext = useMemo<SlashArgContext | null>(() => {
    if (!input.startsWith("/")) return null;
    if (slashMatches !== null) return null;
    return detectSlashArgContext(input, !!codeMode);
  }, [input, slashMatches, codeMode]);

  // Path completion: async directory listing for `argCompleter: "path"`.
  const slashArgPathCandidates = usePathCandidates(
    rootDir,
    slashArgContext?.kind === "picker" && slashArgContext.spec.argCompleter === "path"
      ? slashArgContext.partial
      : null,
    slashArgContext?.kind === "picker" && slashArgContext.spec.argCompleter === "path",
  );

  const slashArgMatches = useMemo<readonly string[] | null>(() => {
    if (!slashArgContext || slashArgContext.kind !== "picker") return null;
    const completer = slashArgContext.spec.argCompleter;
    const partial = slashArgContext.partial;
    const needle = partial.toLowerCase();
    if (Array.isArray(completer)) {
      if (partial && completer.some((v) => v.toLowerCase() === needle)) return null;
      if (!partial) return completer.slice();
      return completer.filter((v) => v.toLowerCase().startsWith(needle));
    }
    if (completer === "models") {
      const all = models ?? [];
      if (partial && all.some((m) => m.toLowerCase() === needle)) return null;
      if (!partial) return all.slice(0, 40);
      return all.filter((m) => m.toLowerCase().includes(needle)).slice(0, 40);
    }
    if (completer === "mcp-resources") {
      const uris: string[] = [];
      const servers = mcpServers ?? [];
      for (const s of servers) {
        if (!s.report.resources.supported) continue;
        for (const r of s.report.resources.items) uris.push(r.uri);
      }
      if (partial && uris.some((u) => u.toLowerCase() === needle)) return null;
      if (!partial) return uris.slice(0, 40);
      return uris.filter((u) => u.toLowerCase().includes(needle)).slice(0, 40);
    }
    if (completer === "mcp-prompts") {
      const names: string[] = [];
      const servers = mcpServers ?? [];
      for (const s of servers) {
        if (!s.report.prompts.supported) continue;
        for (const p of s.report.prompts.items) names.push(p.name);
      }
      if (partial && names.some((n) => n.toLowerCase() === needle)) return null;
      if (!partial) return names.slice(0, 40);
      return names.filter((n) => n.toLowerCase().includes(needle)).slice(0, 40);
    }
    if (completer === "skills") {
      const store = new SkillStore({ projectRoot: codeMode?.rootDir });
      const names = store
        .list()
        .filter((s) => s.scope !== "builtin")
        .map((s) => s.name);
      if (partial && names.some((n) => n.toLowerCase() === needle)) return null;
      if (!partial) return names.slice(0, 40);
      return names.filter((n) => n.toLowerCase().includes(needle)).slice(0, 40);
    }
    if (completer === "path") {
      // Async-listed entries — map the rich candidates to strings.
      // `slashArgPathCandidates` is populated by `usePathCandidates`;
      // this memo re-fires when the async listing completes.
      return slashArgPathCandidates.map((e) => e.insertPath);
    }
    return null;
  }, [slashArgContext, models, mcpServers, codeMode, slashArgPathCandidates]);
  useEffect(() => {
    setSlashArgSelected((prev) => {
      if (!slashArgMatches || slashArgMatches.length === 0) return 0;
      if (prev >= slashArgMatches.length) return slashArgMatches.length - 1;
      return prev;
    });
  }, [slashArgMatches]);
  const pickSlashArg = useCallback(
    (chosen: string, isDir?: boolean) => {
      if (!slashArgContext) return;
      const before = input.slice(0, slashArgContext.partialOffset);
      if (isDir === true) {
        // Directory drill-down — trailing slash re-opens the picker for deeper navigation.
        setInput(`${before}${chosen}/`);
      } else if (isDir === false) {
        // Leaf commit — trailing space closes the picker (the user lands on Enter-to-run).
        setInput(`${before}${chosen} `);
      } else {
        // Non-path completer (models, skills, etc.) — no suffix.
        setInput(`${before}${chosen}`);
      }
    },
    [slashArgContext, input, setInput],
  );

  return {
    slashMatches,
    slashSelected,
    setSlashSelected,
    slashGroupMode,
    slashAdvancedHidden,
    atState,
    atSelected,
    setAtSelected,
    pickAtMention,
    recordRecentFile,
    slashArgContext,
    slashArgMatches,
    slashArgSelected,
    setSlashArgSelected,
    slashArgPathCandidates,
    pickSlashArg,
  };
}

/** Async directory listing for `argCompleter: "path"` (e.g. `/cwd`). Lists entries, sorts dirs first, caps at SEARCH_RESULT_CAP. */
function usePathCandidates(
  rootDir: string,
  partial: string | null,
  isActive: boolean,
): readonly AtPickerEntry[] {
  const [entries, setEntries] = useState<readonly AtPickerEntry[]>([]);

  useEffect(() => {
    if (!isActive) {
      setEntries([]);
      return;
    }
    if (partial === null) {
      setEntries([]);
      return;
    }
    let cancelled = false;

    // Resolve the partial to a (listRoot, relPartial) pair.
    // Absolute paths list from filesystem root.  Relative paths that escape
    // rootDir via `..` resolve to absolute and list from / instead.
    const hasTrailingSlash = partial.endsWith("/");
    let listRoot: string;
    let relPartial: string;
    let insertIsAbsolute: boolean;

    if (isAbsolute(partial)) {
      // Absolute path → list from the drive root (C:\ on Windows, / on POSIX).
      const root = parse(partial).root;
      listRoot = root;
      relPartial = partial.slice(root.length).replace(/\\/g, "/");
      insertIsAbsolute = true;
    } else {
      // Relative path — check whether it escapes rootDir.
      const resolved = resolve(rootDir, partial);
      const relToRoot = relative(rootDir, resolved);
      if (relToRoot.startsWith("..") || isAbsolute(relToRoot)) {
        // Escapes rootDir — resolve to absolute and list from the drive root.
        const root = parse(resolved).root;
        listRoot = root;
        relPartial = resolved.slice(root.length).replace(/\\/g, "/");
        if (hasTrailingSlash && !relPartial.endsWith("/")) relPartial += "/";
        insertIsAbsolute = true;
      } else {
        // Stays inside rootDir — normal relative listing.
        listRoot = rootDir;
        relPartial = partial;
        insertIsAbsolute = false;
      }
    }

    // Parse the partial argument to split parent-dir from filename filter.
    const parsed = parseAtQuery(relPartial);
    const dir = parsed.dir; // parent directory (empty = root)
    const filter = parsed.filter.toLowerCase();

    listDirectory(listRoot, dir)
      .then((raw) => {
        if (cancelled) return;

        // Only directories — /cwd only accepts directory paths.
        let result: DirEntry[] = raw.filter((e) => e.isDir);
        if (filter) {
          result = result.filter((e) => e.name.toLowerCase().startsWith(filter));
        }

        // Cap to match @-mention search so a directory with 10k+ entries
        // doesn't blow out memory or block the UI on a single render.
        if (result.length > SEARCH_RESULT_CAP) {
          result = result.slice(0, SEARCH_RESULT_CAP);
        }

        const listRootNorm = listRoot.replace(/\\/g, "/");
        const mapped: AtPickerEntry[] = result.map((e) => ({
          label: e.name,
          insertPath: insertIsAbsolute
            ? dir
              ? `${listRootNorm}${dir}/${e.name}`
              : `${listRootNorm}${e.name}`
            : dir
              ? `${dir}/${e.name}`
              : e.name,
          dirSuffix: "",
          isDir: e.isDir,
        }));

        // Directories first, then files; alpha within each group.
        mapped.sort((a, b) => {
          if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
          return a.label.localeCompare(b.label);
        });

        setEntries(mapped);
      })
      .catch(() => {
        if (!cancelled) setEntries([]);
      });

    return () => {
      cancelled = true;
    };
  }, [rootDir, partial, isActive]);

  return entries;
}

function useBrowseListing(rootDir: string, dir: string | null) {
  const [entries, setEntries] = useState<readonly AtPickerEntry[]>([]);
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (dir === null) {
      setEntries([]);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    listDirectory(rootDir, dir).then(
      (raw) => {
        if (cancelled) return;
        setEntries(raw.map(toBrowseEntry));
        setLoading(false);
      },
      () => {
        if (cancelled) return;
        setEntries([]);
        setLoading(false);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [rootDir, dir]);
  return { entries, loading };
}

function toBrowseEntry(d: DirEntry): AtPickerEntry {
  return { label: d.name, insertPath: d.path, dirSuffix: "", isDir: d.isDir };
}

function useStreamingSearch(
  rootDir: string,
  filter: string | null,
  recentFilesRef: React.RefObject<string[]>,
) {
  const [, bumpRender] = useReducer((x: number) => x + 1, 0);
  const hitsRef = useRef<FileWithStats[]>([]);
  const scannedRef = useRef(0);
  const searchingRef = useRef(false);
  const rankedRef = useRef<readonly AtPickerEntry[]>([]);

  useEffect(() => {
    if (filter === null) {
      hitsRef.current = [];
      scannedRef.current = 0;
      searchingRef.current = false;
      rankedRef.current = [];
      bumpRender();
      return;
    }
    hitsRef.current = [];
    scannedRef.current = 0;
    searchingRef.current = true;
    rankedRef.current = [];
    bumpRender();

    const ac = new AbortController();
    let flushTimer: ReturnType<typeof setTimeout> | null = null;
    const scheduleFlush = () => {
      if (flushTimer) return;
      flushTimer = setTimeout(() => {
        flushTimer = null;
        rankedRef.current = rankSearchHits(hitsRef.current, filter, recentFilesRef.current ?? []);
        bumpRender();
      }, SEARCH_FLUSH_MS);
    };

    const debounce = setTimeout(() => {
      walkFilesStream(rootDir, {
        signal: ac.signal,
        onEntry: (e) => {
          hitsRef.current.push(e);
          if (hitsRef.current.length >= SEARCH_RESULT_CAP * 8) return false;
          scheduleFlush();
        },
        onProgress: (n) => {
          scannedRef.current = n;
          scheduleFlush();
        },
      }).then(() => {
        searchingRef.current = false;
        rankedRef.current = rankSearchHits(hitsRef.current, filter, recentFilesRef.current ?? []);
        bumpRender();
      });
    }, SEARCH_DEBOUNCE_MS);

    return () => {
      clearTimeout(debounce);
      if (flushTimer) clearTimeout(flushTimer);
      ac.abort();
    };
  }, [rootDir, filter, recentFilesRef]);

  return {
    entries: rankedRef.current,
    scanned: scannedRef.current,
    searching: searchingRef.current,
  };
}

function rankSearchHits(
  hits: readonly FileWithStats[],
  filter: string,
  recent: readonly string[],
): readonly AtPickerEntry[] {
  const ranked = rankPickerCandidates(hits, filter, {
    limit: SEARCH_RESULT_CAP,
    recentlyUsed: recent,
  });
  return ranked.map((path) => {
    const slash = path.lastIndexOf("/");
    return {
      label: slash >= 0 ? path.slice(slash + 1) : path,
      insertPath: path,
      dirSuffix: slash >= 0 ? `${path.slice(0, slash)}/` : "",
      isDir: false,
    };
  });
}
