/** `reasonix mcp browse` — Ink TUI for the MCP marketplace. Lazy-loads pages on scroll. */

import { Box, Text, render, useApp, useInput } from "ink";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { readConfig, writeConfig } from "../../config.js";
import { loadDotenv } from "../../env.js";
import {
  type RegistryHandle,
  loadMorePages,
  openRegistry,
  specStringFor,
} from "../../mcp/registry-fetch.js";
import type { RegistryEntry } from "../../mcp/registry-types.js";

const VISIBLE_ROWS = 12;

interface State {
  handle: RegistryHandle | null;
  loading: boolean;
  query: string;
  selected: number;
  status: string;
}

function rankAndFilter(entries: RegistryEntry[], query: string): RegistryEntry[] {
  const q = query.trim().toLowerCase();
  const list = q
    ? entries.filter((e) => `${e.name} ${e.title} ${e.description}`.toLowerCase().includes(q))
    : entries;
  return [...list].sort((a, b) => {
    const ap = a.popularity ?? -1;
    const bp = b.popularity ?? -1;
    if (ap !== bp) return bp - ap;
    return a.name.localeCompare(b.name);
  });
}

function McpBrowseApp() {
  const app = useApp();
  const [state, setState] = useState<State>({
    handle: null,
    loading: true,
    query: "",
    selected: 0,
    status: "opening registry…",
  });

  const setStatus = useCallback((status: string) => {
    setState((s) => ({ ...s, status }));
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const handle = await openRegistry({});
        if (cancelled) return;
        const ageMs = Date.now() - handle.fetchedAt;
        const ageStr =
          ageMs < 60_000 ? `${Math.floor(ageMs / 1000)}s` : `${Math.floor(ageMs / 60_000)}m`;
        setState((s) => ({
          ...s,
          handle,
          loading: false,
          status: `${handle.source} · ${handle.cache.entries.length} entries${
            handle.fromCache ? ` · cached ${ageStr} ago` : ""
          }`,
        }));
      } catch (err) {
        if (cancelled) return;
        setState((s) => ({ ...s, loading: false, status: `error: ${(err as Error).message}` }));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const filtered = useMemo(() => {
    if (!state.handle) return [];
    return rankAndFilter(state.handle.cache.entries, state.query);
  }, [state.handle, state.query]);

  const selected = filtered[state.selected];

  const fetchMore = useCallback(async () => {
    if (!state.handle || state.loading) return;
    if (state.handle.cache.pagination.nextCursor === null) {
      setStatus("no more pages — registry exhausted");
      return;
    }
    setState((s) => ({ ...s, loading: true, status: "loading more…" }));
    try {
      const r = await loadMorePages(state.handle, { pages: 5 });
      setState((s) => ({
        ...s,
        loading: false,
        status: `+${r.newEntries} entries (${state.handle?.cache.entries.length ?? 0} total)${
          r.exhausted ? " · exhausted" : ""
        }`,
      }));
    } catch (err) {
      setState((s) => ({ ...s, loading: false, status: `error: ${(err as Error).message}` }));
    }
  }, [state.handle, state.loading, setStatus]);

  const install = useCallback(
    (entry: RegistryEntry) => {
      if (!entry.install) {
        setStatus(`${entry.name} has no install info (smithery listing)`);
        return;
      }
      try {
        const spec = specStringFor(entry.name, entry.install);
        const cfg = readConfig();
        const existing = cfg.mcp ?? [];
        if (existing.includes(spec)) {
          setStatus(`already installed: ${spec}`);
          return;
        }
        writeConfig({ ...cfg, mcp: [...existing, spec] });
        setStatus(`installed → ${spec}`);
      } catch (err) {
        setStatus(`install failed: ${(err as Error).message}`);
      }
    },
    [setStatus],
  );

  useInput((input, key) => {
    if (key.escape || (key.ctrl && input === "c")) {
      app.exit();
      return;
    }
    if (key.upArrow) {
      setState((s) => ({ ...s, selected: Math.max(0, s.selected - 1) }));
      return;
    }
    if (key.downArrow) {
      setState((s) => ({ ...s, selected: Math.min(filtered.length - 1, s.selected + 1) }));
      return;
    }
    if (key.return) {
      if (selected) install(selected);
      return;
    }
    if (key.tab || (key.ctrl && input === "n")) {
      void fetchMore();
      return;
    }
    if (key.backspace || key.delete) {
      setState((s) => ({ ...s, query: s.query.slice(0, -1), selected: 0 }));
      return;
    }
    if (input && !key.ctrl && !key.meta) {
      setState((s) => ({ ...s, query: s.query + input, selected: 0 }));
    }
  });

  const start = Math.max(
    0,
    Math.min(state.selected - Math.floor(VISIBLE_ROWS / 2), filtered.length - VISIBLE_ROWS),
  );
  const window = filtered.slice(Math.max(0, start), Math.max(0, start) + VISIBLE_ROWS);

  return (
    <Box flexDirection="column" paddingX={1}>
      <Box>
        <Text bold color="cyan">
          ◈ MCP marketplace
        </Text>
        <Text dimColor>{`  ·  ${state.status}`}</Text>
      </Box>
      <Box marginTop={1}>
        <Text>search: </Text>
        <Text color="white">{state.query || "(type to filter)"}</Text>
        <Text dimColor>{`  ${filtered.length} match${filtered.length === 1 ? "" : "es"}`}</Text>
      </Box>
      <Box marginTop={1} flexDirection="column">
        {window.length === 0 ? (
          <Text dimColor>{state.loading ? "loading…" : "no entries"}</Text>
        ) : (
          window.map((e, i) => {
            const idx = (start || 0) + i;
            const active = idx === state.selected;
            const tag =
              e.source === "official" ? "[off]" : e.source === "smithery" ? "[smt]" : "[loc]";
            const pop = e.popularity !== undefined ? ` · ${e.popularity.toLocaleString()}` : "";
            return (
              <Box key={e.name}>
                <Text color={active ? "cyan" : undefined}>{active ? "▸ " : "  "}</Text>
                <Text bold={active}>{e.name.padEnd(40).slice(0, 40)}</Text>
                <Text dimColor>{` ${tag}${pop}`}</Text>
              </Box>
            );
          })
        )}
      </Box>
      {selected ? (
        <Box marginTop={1} flexDirection="column">
          <Text bold color="white">
            {selected.title}
          </Text>
          {selected.description ? <Text dimColor>{selected.description.slice(0, 160)}</Text> : null}
          {selected.install ? (
            <Text dimColor>
              {`spec: ${selected.install.runtime} ${selected.install.packageId ?? selected.install.url ?? "—"} · ${selected.install.transport}`}
            </Text>
          ) : (
            <Text dimColor>(smithery listing — install info not exposed)</Text>
          )}
          {selected.install?.requiredEnv?.length ? (
            <Text color="yellow">{`needs: ${selected.install.requiredEnv.join(", ")}`}</Text>
          ) : null}
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text dimColor>type to filter · ↑↓ pick · enter install · tab load more · esc quit</Text>
      </Box>
    </Box>
  );
}

export interface McpBrowseOptions {
  /** Reserved — currently unused, kept for symmetry with other commands. */
  _unused?: never;
}

export async function mcpBrowseCommand(_opts: McpBrowseOptions = {}): Promise<void> {
  loadDotenv();
  const { waitUntilExit } = render(<McpBrowseApp />, {
    exitOnCtrlC: true,
    patchConsole: false,
  });
  await waitUntilExit();
}
