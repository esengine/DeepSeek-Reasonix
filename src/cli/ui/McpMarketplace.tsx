/** `/mcp browse` modal — registry marketplace inside the chat session. */

import { Box, Text } from "ink";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { readConfig, writeConfig } from "../../config.js";
import {
  type RegistryHandle,
  fetchSmitheryDetail,
  loadMorePages,
  openRegistry,
  specStringFor,
} from "../../mcp/registry-fetch.js";
import type { RegistryEntry } from "../../mcp/registry-types.js";
import { type PickerBroadcastPorts, usePickerBroadcast } from "./dashboard/use-picker-broadcast.js";
import { useKeystroke } from "./keystroke-context.js";
import { COLOR } from "./theme.js";

const VISIBLE_ROWS = 10;

export interface McpMarketplaceProps {
  onClose: () => void;
  /** Pushed back into the chat scrollback after install/uninstall. */
  postInfo: (text: string) => void;
  /** Optional hot-reload — present in chat session, absent in standalone CLI use. */
  reloadMcp?: () => Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
  }>;
  pickerPorts?: PickerBroadcastPorts;
}

interface State {
  handle: RegistryHandle | null;
  loading: boolean;
  query: string;
  selected: number;
  status: string;
  /** specs currently in config.mcp[] — refreshed after install/uninstall. */
  installedSpecs: string[];
}

export function buildMarketplacePickerSnapshot(args: {
  filtered: RegistryEntry[];
  installedSpecs: string[];
  query: string;
  status: string;
  hasMore: boolean;
}) {
  return {
    pickerKind: "mcp-marketplace" as const,
    title: `MCP marketplace · ${args.status}`,
    query: args.query,
    items: args.filtered.map((e) => {
      const installedSpec = isInstalled(args.installedSpecs, e);
      return {
        id: e.name,
        title: e.title || e.name,
        subtitle: e.description?.slice(0, 200) ?? undefined,
        badge: installedSpec
          ? "installed"
          : e.source === "official"
            ? "official"
            : e.source === "smithery"
              ? "smithery"
              : "local",
        meta: e.popularity !== undefined ? `★ ${e.popularity.toLocaleString()}` : undefined,
      };
    }),
    actions: ["install", "uninstall", "refine", "load-more", "cancel"] as const,
    hasMore: args.hasMore,
    hint: "type filter · ↑↓ pick · ⏎ install/toggle · PgDn load more · esc close",
  };
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

function readInstalledSpecs(): string[] {
  return readConfig().mcp ?? [];
}

function isInstalled(installedSpecs: string[], entry: RegistryEntry): string | null {
  if (!entry.install) return null;
  try {
    const spec = specStringFor(entry.name, entry.install);
    return installedSpecs.includes(spec) ? spec : null;
  } catch {
    return null;
  }
}

export function McpMarketplace({ onClose, postInfo, reloadMcp, pickerPorts }: McpMarketplaceProps) {
  const [state, setState] = useState<State>({
    handle: null,
    loading: true,
    query: "",
    selected: 0,
    status: "opening registry…",
    installedSpecs: readInstalledSpecs(),
  });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const handle = await openRegistry({});
        if (cancelled) return;
        setState((s) => ({
          ...s,
          handle,
          loading: false,
          status: `${handle.source} · ${handle.cache.entries.length} entries${
            handle.fromCache ? " · cached" : ""
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
      setState((s) => ({ ...s, status: "all pages loaded" }));
      return;
    }
    setState((s) => ({ ...s, loading: true, status: "loading more…" }));
    try {
      const r = await loadMorePages(state.handle, { pages: 5 });
      setState((s) => ({
        ...s,
        loading: false,
        status: `+${r.newEntries} · ${state.handle?.cache.entries.length ?? 0} total${
          r.exhausted ? " · exhausted" : ""
        }`,
      }));
    } catch (err) {
      setState((s) => ({ ...s, loading: false, status: `error: ${(err as Error).message}` }));
    }
  }, [state.handle, state.loading]);

  const doUninstall = useCallback(
    async (entry: RegistryEntry, installed: string) => {
      const cfg = readConfig();
      const next = (cfg.mcp ?? []).filter((s) => s !== installed);
      writeConfig({ ...cfg, mcp: next });
      setState((s) => ({ ...s, installedSpecs: next, status: `uninstalled ${entry.name}` }));
      if (reloadMcp) {
        try {
          await reloadMcp();
          postInfo(`✓ uninstalled ${entry.name} — bridge dropped`);
        } catch (err) {
          postInfo(
            `✓ uninstalled ${entry.name} — restart \`reasonix code\` to drop the bridge (reload failed: ${(err as Error).message})`,
          );
        }
      } else {
        postInfo(`✓ uninstalled ${entry.name} — restart \`reasonix code\` to drop the bridge`);
      }
    },
    [postInfo, reloadMcp],
  );

  const doInstall = useCallback(
    async (entry: RegistryEntry) => {
      let install = entry.install;
      if (!install && entry.source === "smithery") {
        setState((s) => ({ ...s, loading: true, status: "fetching smithery detail…" }));
        try {
          const detail = await fetchSmitheryDetail(entry.name);
          if (detail) {
            install = detail;
            entry.install = detail;
          }
        } catch {
          /* fall through to error below */
        }
        setState((s) => ({ ...s, loading: false }));
      }
      if (!install) {
        setState((s) => ({
          ...s,
          status: `no install info for ${entry.name} — try \`npx -y @smithery/cli install ${entry.name}\``,
        }));
        return;
      }
      try {
        const spec = specStringFor(entry.name, install);
        const cfg = readConfig();
        const existing = cfg.mcp ?? [];
        if (existing.includes(spec)) {
          setState((s) => ({
            ...s,
            installedSpecs: existing,
            status: `already installed: ${spec}`,
          }));
          return;
        }
        const next = [...existing, spec];
        writeConfig({ ...cfg, mcp: next });
        setState((s) => ({ ...s, installedSpecs: next, status: `installed → ${spec}` }));
        const envHint = install.requiredEnv?.length
          ? `  ·  needs env: ${install.requiredEnv.join(", ")}`
          : "";
        if (reloadMcp) {
          try {
            const r = await reloadMcp();
            const failedHere = r.failed.find((f) => f.spec === spec);
            if (failedHere) {
              postInfo(`▲ installed ${entry.name} — bridge failed: ${failedHere.reason}${envHint}`);
            } else {
              postInfo(`✓ installed ${entry.name} — bridged${envHint}`);
            }
          } catch (err) {
            postInfo(
              `✓ installed ${entry.name} — restart \`reasonix code\` to bridge (reload failed: ${(err as Error).message})${envHint}`,
            );
          }
        } else {
          postInfo(`✓ installed ${entry.name} — restart \`reasonix code\` to bridge${envHint}`);
        }
      } catch (err) {
        setState((s) => ({ ...s, status: `install failed: ${(err as Error).message}` }));
      }
    },
    [postInfo, reloadMcp],
  );

  const installOrToggle = useCallback(
    async (entry: RegistryEntry) => {
      const installed = isInstalled(state.installedSpecs, entry);
      if (installed) await doUninstall(entry, installed);
      else await doInstall(entry);
    },
    [state.installedSpecs, doInstall, doUninstall],
  );

  const pickerSnapshot = useMemo(
    () =>
      buildMarketplacePickerSnapshot({
        filtered,
        installedSpecs: state.installedSpecs,
        query: state.query,
        status: state.status,
        hasMore: state.handle?.cache.pagination.nextCursor != null,
      }),
    [filtered, state.installedSpecs, state.handle, state.query, state.status],
  );

  usePickerBroadcast(
    !!pickerPorts,
    { ...pickerSnapshot, actions: [...pickerSnapshot.actions] },
    (res) => {
      if (res.action === "cancel") return onClose();
      if (res.action === "refine") {
        setState((s) => ({ ...s, query: res.query, selected: 0 }));
        return;
      }
      if (res.action === "load-more") {
        void fetchMore();
        return;
      }
      if (res.action === "install") {
        const entry = state.handle?.cache.entries.find((e) => e.name === res.id);
        if (!entry) return;
        if (isInstalled(state.installedSpecs, entry)) {
          setState((s) => ({ ...s, status: `already installed: ${entry.name}` }));
          return;
        }
        void doInstall(entry);
        return;
      }
      if (res.action === "uninstall") {
        const entry = state.handle?.cache.entries.find((e) => e.name === res.id);
        if (!entry) return;
        const installed = isInstalled(state.installedSpecs, entry);
        if (!installed) {
          setState((s) => ({ ...s, status: `not installed: ${entry.name}` }));
          return;
        }
        void doUninstall(entry, installed);
      }
    },
    pickerPorts ?? {
      broadcast: () => undefined,
      resolverRef: { current: null },
      snapshotRef: { current: null },
    },
  );

  useKeystroke((ev) => {
    if (ev.paste) return;
    if (ev.escape) {
      onClose();
      return;
    }
    if (ev.upArrow) {
      setState((s) => ({ ...s, selected: Math.max(0, s.selected - 1) }));
      return;
    }
    if (ev.downArrow) {
      setState((s) => ({ ...s, selected: Math.min(filtered.length - 1, s.selected + 1) }));
      return;
    }
    if (ev.return) {
      if (selected) void installOrToggle(selected);
      return;
    }
    if (ev.pageDown) {
      void fetchMore();
      return;
    }
    if (ev.backspace || ev.delete) {
      setState((s) => ({ ...s, query: s.query.slice(0, -1), selected: 0 }));
      return;
    }
    if (ev.input && !ev.ctrl && !ev.meta) {
      setState((s) => ({ ...s, query: s.query + ev.input, selected: 0 }));
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
        <Text bold color={COLOR.brand}>
          ◈ MCP marketplace
        </Text>
        <Text dimColor>{`  ·  ${state.status}`}</Text>
      </Box>
      <Box marginTop={1}>
        <Text>filter: </Text>
        <Text>{state.query || "(type to filter)"}</Text>
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
            const installedSpec = isInstalled(state.installedSpecs, e);
            const installedBadge = installedSpec ? " ✓" : "";
            const pop = e.popularity !== undefined ? ` · ${e.popularity.toLocaleString()}` : "";
            return (
              <Box key={e.name}>
                <Text color={active ? COLOR.brand : undefined}>{active ? "▸ " : "  "}</Text>
                <Text bold={active}>{e.name.padEnd(38).slice(0, 38)}</Text>
                <Text dimColor>{` ${tag}${pop}${installedBadge}`}</Text>
              </Box>
            );
          })
        )}
      </Box>
      {selected ? (
        <Box marginTop={1} flexDirection="column">
          <Text bold>{selected.title}</Text>
          {selected.description ? <Text dimColor>{selected.description.slice(0, 200)}</Text> : null}
          {selected.install ? (
            <Text dimColor>
              {`spec: ${selected.install.runtime} ${selected.install.packageId ?? selected.install.url ?? "—"} · ${selected.install.transport}`}
            </Text>
          ) : (
            <Text dimColor>(smithery listing — install detail fetched on Enter)</Text>
          )}
          {selected.install?.requiredEnv?.length ? (
            <Text color="yellow">{`needs: ${selected.install.requiredEnv.join(", ")}`}</Text>
          ) : null}
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text dimColor>
          type filter · ↑↓ pick · enter{" "}
          {selected && isInstalled(state.installedSpecs, selected) ? "uninstall" : "install"} · PgDn
          load more · esc close
        </Text>
      </Box>
    </Box>
  );
}
