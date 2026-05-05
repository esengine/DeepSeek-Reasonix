import { useCallback, useEffect, useState } from "preact/hooks";
import { t, useLang } from "../i18n/index.js";
import { api } from "../lib/api.js";
import { fmtNum } from "../lib/format.js";
import { html } from "../lib/html.js";

interface McpServer {
  label: string;
  spec: string;
  serverInfo?: { name?: string; version?: string };
  protocolVersion?: string;
  instructions?: string;
  toolCount: number;
  tools: { name: string; description?: string }[];
  resources: { name: string; uri: string }[];
  prompts: { name: string; description?: string }[];
}

interface McpData {
  servers: McpServer[];
}

interface RegistryInstall {
  runtime: string;
  packageId?: string;
  version?: string;
  transport: string;
  url?: string;
  requiredEnv?: string[];
}

interface RegistryEntryDto {
  name: string;
  title: string;
  description: string;
  source: "official" | "smithery" | "local";
  install?: RegistryInstall;
  popularity?: number;
  homepage?: string;
}

interface RegistryListResponse {
  source: "official" | "smithery" | "local";
  fromCache: boolean;
  fetchedAt: number;
  loaded: number;
  hasMore: boolean;
  matched: number;
  entries: RegistryEntryDto[];
  errors: string[];
}

function specLabel(spec: string): string {
  const eq = spec.indexOf("=");
  return eq > 0 ? spec.slice(0, eq) : spec;
}

function specCommand(spec: string): string {
  const eq = spec.indexOf("=");
  return eq > 0 ? spec.slice(eq + 1) : spec;
}

type McpFilter = "all" | "live" | "unbridged" | "marketplace";

export function McpPanel() {
  useLang();
  const [data, setData] = useState<McpData | null>(null);
  const [specs, setSpecs] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [newSpec, setNewSpec] = useState("");
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState<McpServer | null>(null);
  const [openUnbridged, setOpenUnbridged] = useState<string | null>(null);
  const [filter, setFilter] = useState<McpFilter>("all");
  const [registry, setRegistry] = useState<RegistryListResponse | null>(null);
  const [registryQuery, setRegistryQuery] = useState("");
  const [registryLoading, setRegistryLoading] = useState(false);
  const [openRegistry, setOpenRegistry] = useState<RegistryEntryDto | null>(null);

  const loadRegistry = useCallback(async (q: string, pages: number) => {
    setRegistryLoading(true);
    try {
      const params = new URLSearchParams();
      if (q.trim()) params.set("q", q.trim());
      params.set("pages", String(pages));
      params.set("maxPages", String(Math.max(20, pages)));
      params.set("limit", "50");
      const r = await api<RegistryListResponse>(`/mcp/registry?${params.toString()}`);
      setRegistry(r);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setRegistryLoading(false);
    }
  }, []);

  useEffect(() => {
    if (filter !== "marketplace") return;
    if (registry) return;
    void loadRegistry("", 1);
  }, [filter, registry, loadRegistry]);

  useEffect(() => {
    if (filter !== "marketplace") return;
    const handle = setTimeout(() => void loadRegistry(registryQuery, 1), 300);
    return () => clearTimeout(handle);
  }, [registryQuery, filter, loadRegistry]);

  const installFromRegistry = useCallback(async (entry: RegistryEntryDto) => {
    setBusy(true);
    try {
      const r = await api<{ added: boolean; alreadyPresent?: boolean; spec: string }>(
        "/mcp/registry/install",
        { method: "POST", body: { name: entry.name } },
      );
      if (r.alreadyPresent) {
        setInfo(t("mcp.marketplaceAlready"));
      } else {
        setInfo(t("mcp.marketplaceInstalled", { spec: r.spec }));
      }
      setTimeout(() => setInfo(null), 4000);
      const fresh = await api<{ specs: string[] }>("/mcp/specs");
      setSpecs(fresh.specs);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, []);

  const load = useCallback(async () => {
    try {
      setData(await api<McpData>("/mcp"));
      setSpecs((await api<{ specs: string[] }>("/mcp/specs")).specs);
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);
  useEffect(() => {
    load();
  }, [load]);

  const addSpec = useCallback(async () => {
    if (!newSpec.trim()) return;
    setBusy(true);
    try {
      const r = await api<{ requiresRestart?: boolean }>("/mcp/specs", {
        method: "POST",
        body: { spec: newSpec.trim() },
      });
      setInfo(r.requiresRestart ? t("mcp.savedRestart") : t("mcp.saved"));
      setTimeout(() => setInfo(null), 4000);
      setNewSpec("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, [newSpec, load]);

  const removeSpec = useCallback(
    async (spec: string) => {
      if (!confirm(t("mcp.removeConfirm", { spec }))) return;
      setBusy(true);
      try {
        await api("/mcp/specs", { method: "DELETE", body: { spec } });
        setInfo(t("mcp.removed"));
        setTimeout(() => setInfo(null), 4000);
        await load();
      } catch (err) {
        setError((err as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  if (!data && !error)
    return html`<div class="card" style="color:var(--fg-3)">${t("mcp.loading")}</div>`;
  if (error && !data) return html`<div class="card accent-err">${error}</div>`;
  if (!data) return null;

  const liveCount = data.servers.length;
  const unbridgedSpecs = (specs ?? []).filter((spec) => !data.servers.some((s) => s.spec === spec));
  const unbridgedCount = unbridgedSpecs.length;
  const showLive = filter === "all" || filter === "live";
  const showUnbridged = filter === "all" || filter === "unbridged";
  const showMarketplace = filter === "marketplace";

  return html`
    <div class="sessions-grid">
      <div class="sessions-list">
        <div class="ssl-h" style="font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">
          ${t("mcp.servers", { count: liveCount })}
        </div>
        <div style="padding:8px 12px 4px">
          <div class="chips">
            <span class=${`chip-f ${filter === "all" ? "active" : ""}`} onClick=${() => setFilter("all")}>${t("mcp.all")} <span class="ct">${liveCount + unbridgedCount}</span></span>
            <span class=${`chip-f ${filter === "live" ? "active" : ""}`} onClick=${() => setFilter("live")}>${t("mcp.live")} <span class="ct">${liveCount}</span></span>
            <span class=${`chip-f ${filter === "unbridged" ? "active" : ""}`} onClick=${() => setFilter("unbridged")}>${t("mcp.unbridged")} <span class="ct">${unbridgedCount}</span></span>
            <span class=${`chip-f ${filter === "marketplace" ? "active" : ""}`} onClick=${() => setFilter("marketplace")}>${t("mcp.marketplace")}</span>
          </div>
        </div>
        ${
          showMarketplace
            ? html`
              <div style="padding:8px 12px;display:flex;gap:6px">
                <input
                  type="text"
                  placeholder=${t("mcp.marketplaceSearch")}
                  value=${registryQuery}
                  onInput=${(e: Event) => setRegistryQuery((e.target as HTMLInputElement).value)}
                  style="flex:1;font-size:11px"
                />
              </div>
              ${
                registry
                  ? html`<div style="padding:0 12px 6px;font-size:11px;color:var(--fg-3)">
                      ${t("mcp.marketplaceCount", {
                        loaded: registry.loaded,
                        matched: registry.matched,
                        source: registry.source,
                        cached: registry.fromCache ? t("mcp.marketplaceCachedSuffix") : "",
                      })}
                    </div>`
                  : null
              }
            `
            : html`
              <div style="padding:8px 12px;display:flex;gap:6px">
                <input
                  type="text"
                  placeholder=${t("mcp.specPlaceholder")}
                  value=${newSpec}
                  onInput=${(e: Event) => setNewSpec((e.target as HTMLInputElement).value)}
                  style="flex:1;font-size:11px"
                />
                <button class="btn primary" disabled=${busy || !newSpec.trim()} onClick=${addSpec}>+</button>
              </div>
            `
        }
        ${info ? html`<div style="padding:0 12px 8px"><span class="pill ok">${info}</span></div>` : null}
        ${error ? html`<div class="card accent-err" style="margin:0 12px 8px">${error}</div>` : null}

        <div class="ssl-rows">
          ${
            !showMarketplace && liveCount === 0 && unbridgedCount === 0
              ? html`<div style="color:var(--fg-3);padding:14px;font-size:12px">
                ${t("mcp.noServers")}
              </div>`
              : null
          }
          ${
            showMarketplace
              ? renderMarketplaceRows({
                  registry,
                  registryLoading,
                  openRegistry,
                  setOpenRegistry: (e) => {
                    setOpenRegistry(e);
                    setOpen(null);
                    setOpenUnbridged(null);
                  },
                  loadMore: () => loadRegistry(registryQuery, (registry?.loaded ?? 0) / 30 + 5),
                })
              : null
          }
          ${
            showLive
              ? data.servers.map(
                  (s) => html`
                  <div
                    class=${`ssl-row ${open?.label === s.label ? "sel" : ""}`}
                    onClick=${() => {
                      setOpen(s);
                      setOpenUnbridged(null);
                    }}
                  >
                    <span class="name">${s.label} <span class="pill ok">${t("mcp.live")}</span></span>
                    <span class="preview">${specCommand(s.spec)}</span>
                    <span class="meta"><span><span class="v">${fmtNum(s.toolCount)}</span> ${t("mcp.tools")}</span></span>
                  </div>
                `,
                )
              : null
          }
          ${
            showUnbridged
              ? unbridgedSpecs.map(
                  (spec) => html`
                  <div
                    class=${`ssl-row ${openUnbridged === spec ? "sel" : ""}`}
                    onClick=${() => {
                      setOpenUnbridged(spec);
                      setOpen(null);
                    }}
                  >
                    <span class="name">${specLabel(spec)} <span class="pill">${t("mcp.unbridged")}</span></span>
                    <span class="preview">${specCommand(spec)}</span>
                    <span class="meta"><span class="dim">${t("mcp.inConfig")}</span></span>
                  </div>
                `,
                )
              : null
          }
        </div>
      </div>

      <div class="sessions-detail">
        ${
          openRegistry != null
            ? renderRegistryDetail({
                entry: openRegistry,
                busy,
                onInstall: () => installFromRegistry(openRegistry),
                onClose: () => setOpenRegistry(null),
              })
            : openUnbridged != null
              ? html`
              <div class="sessions-detail-h">
                <span class="name">${specLabel(openUnbridged)}</span>
                <span class="ws"><span class="pill">${t("mcp.unbridgedTitle")}</span></span>
                <span class="actions">
                  <button class="btn" disabled=${busy} onClick=${() => removeSpec(openUnbridged)}
                    style="border-color:var(--c-err);color:var(--c-err)">${t("mcp.removeBtn")}</button>
                  <button class="btn ghost" onClick=${() => setOpenUnbridged(null)}>${t("common.back")}</button>
                </span>
              </div>
              <div class="card" style="margin-bottom:12px">
                <div class="card-h"><span class="title">${t("mcp.spec")}</span></div>
                <code class="mono" style="font-size:11.5px;color:var(--fg-2);word-break:break-all">${openUnbridged}</code>
              </div>
              <div class="card accent-warn">
                <div class="card-h"><span class="title" style="color:var(--c-warn)">${t("mcp.whyUnbridged")}</span></div>
                <div class="card-b" style="font-size:13px;line-height:1.6">
                  ${t("mcp.whyUnbridgedDesc")}
                  <div style="margin-top:10px;color:var(--fg-3);font-size:12px">
                    ${t("mcp.whyUnbridgedHint")}
                  </div>
                </div>
              </div>
            `
              : open == null
                ? html`<div style="color:var(--fg-3);font-size:13px;text-align:center;padding:60px 20px">
                ${showMarketplace ? t("mcp.marketplacePickHint") : t("mcp.pickHint")}
              </div>`
                : html`
                <div class="sessions-detail-h">
                  <span class="name">${open.label}</span>
                  <span class="ws">${open.serverInfo?.name ?? "—"} ${open.serverInfo?.version ? `v${open.serverInfo.version}` : ""} · ${open.protocolVersion ?? "—"}</span>
                  <span class="actions">
                    <button class="btn ghost" onClick=${() => setOpen(null)}>${t("common.back")}</button>
                  </span>
                </div>

                <div class="card" style="margin-bottom:12px">
                  <div class="card-h"><span class="title">${t("mcp.spec")}</span></div>
                  <code class="mono" style="font-size:11.5px;color:var(--fg-2)">${open.spec}</code>
                </div>

                ${
                  open.instructions
                    ? html`<div class="card accent-brand" style="margin-bottom:12px">
                        <div class="card-b">${open.instructions}</div>
                      </div>`
                    : null
                }

                <h3 style="margin:18px 0 6px;font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">
                  ${t("mcp.toolsTitle", { count: open.tools.length })}
                </h3>
                <div class="card" style="padding:0;overflow:hidden">
                  <table class="tbl">
                    <thead><tr><th>${t("mcp.colName")}</th><th>${t("mcp.colDesc")}</th></tr></thead>
                    <tbody>
                      ${open.tools.map(
                        (tool) =>
                          html`<tr><td><code class="mono">${tool.name}</code></td><td class="dim">${tool.description ?? ""}</td></tr>`,
                      )}
                    </tbody>
                  </table>
                </div>

                ${
                  open.resources.length > 0
                    ? html`
                      <h3 style="margin:18px 0 6px;font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">
                        ${t("mcp.resourcesTitle", { count: open.resources.length })}
                      </h3>
                      <div class="card" style="padding:0;overflow:hidden">
                        <table class="tbl">
                          <thead><tr><th>${t("mcp.colName")}</th><th>${t("mcp.colUri")}</th></tr></thead>
                          <tbody>
                            ${open.resources.map(
                              (r) =>
                                html`<tr><td>${r.name}</td><td class="path">${r.uri}</td></tr>`,
                            )}
                          </tbody>
                        </table>
                      </div>
                    `
                    : null
                }

                ${
                  open.prompts.length > 0
                    ? html`
                      <h3 style="margin:18px 0 6px;font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">
                        ${t("mcp.promptsTitle", { count: open.prompts.length })}
                      </h3>
                      <div class="card" style="padding:0;overflow:hidden">
                        <table class="tbl">
                          <thead><tr><th>${t("mcp.colName")}</th><th>${t("mcp.colDesc")}</th></tr></thead>
                          <tbody>
                            ${open.prompts.map(
                              (p) =>
                                html`<tr><td><code class="mono">${p.name}</code></td><td class="dim">${p.description ?? ""}</td></tr>`,
                            )}
                          </tbody>
                        </table>
                      </div>
                    `
                    : null
                }
              `
        }
      </div>
    </div>
  `;
}

interface MarketplaceRowsArgs {
  registry: RegistryListResponse | null;
  registryLoading: boolean;
  openRegistry: RegistryEntryDto | null;
  setOpenRegistry: (entry: RegistryEntryDto) => void;
  loadMore: () => void;
}

function renderMarketplaceRows({
  registry,
  registryLoading,
  openRegistry,
  setOpenRegistry,
  loadMore,
}: MarketplaceRowsArgs) {
  if (!registry && registryLoading) {
    return html`<div style="color:var(--fg-3);padding:14px;font-size:12px">${t("mcp.marketplaceLoading")}</div>`;
  }
  if (!registry || registry.entries.length === 0) {
    return html`<div style="color:var(--fg-3);padding:14px;font-size:12px">${t("mcp.marketplaceNoMatches")}</div>`;
  }
  return html`
    ${registry.entries.map((e) => {
      const sel = openRegistry?.name === e.name;
      const tag = t("mcp.marketplaceSourceTag", { source: e.source });
      const pop =
        e.popularity !== undefined
          ? html` <span class="dim">· ${fmtNum(e.popularity)}</span>`
          : null;
      return html`
        <div class=${`ssl-row ${sel ? "sel" : ""}`} onClick=${() => setOpenRegistry(e)}>
          <span class="name">${e.name} <span class="pill">${tag}</span></span>
          <span class="preview">${e.description}</span>
          <span class="meta">${pop}</span>
        </div>
      `;
    })}
    ${
      registry.hasMore
        ? html`<div style="padding:10px 12px">
          <button class="btn" disabled=${registryLoading} onClick=${loadMore}>
            ${registryLoading ? t("mcp.marketplaceLoading") : t("mcp.marketplaceMore")}
          </button>
        </div>`
        : html`<div style="padding:10px 12px;color:var(--fg-3);font-size:11px">${t("mcp.marketplaceExhausted")}</div>`
    }
  `;
}

interface RegistryDetailArgs {
  entry: RegistryEntryDto;
  busy: boolean;
  onInstall: () => void;
  onClose: () => void;
}

function renderRegistryDetail({ entry, busy, onInstall, onClose }: RegistryDetailArgs) {
  const installable = !!entry.install;
  const meta = entry.install
    ? `${entry.install.runtime} · ${entry.install.transport}${
        entry.install.packageId
          ? ` · ${entry.install.packageId}`
          : entry.install.url
            ? ` · ${entry.install.url}`
            : ""
      }${entry.install.version ? `@${entry.install.version}` : ""}`
    : "";
  return html`
    <div class="sessions-detail-h">
      <span class="name">${entry.name}</span>
      <span class="ws">${t("mcp.marketplaceSourceTag", { source: entry.source })}${
        entry.popularity !== undefined ? ` · ${fmtNum(entry.popularity)} uses` : ""
      }</span>
      <span class="actions">
        <button class="btn primary" disabled=${busy || !installable} onClick=${onInstall}>${t("mcp.marketplaceInstall")}</button>
        <button class="btn ghost" onClick=${onClose}>${t("common.back")}</button>
      </span>
    </div>
    <div class="card" style="margin-bottom:12px">
      <div class="card-b" style="font-size:13px;line-height:1.6">${entry.description || "—"}</div>
    </div>
    ${
      installable
        ? html`<div class="card" style="margin-bottom:12px">
            <div class="card-h"><span class="title">${t("mcp.spec")}</span></div>
            <code class="mono" style="font-size:11.5px;color:var(--fg-2);word-break:break-all">${meta}</code>
          </div>`
        : html`<div class="card accent-warn" style="margin-bottom:12px">
            <div class="card-b" style="font-size:13px;line-height:1.6">
              ${t("mcp.marketplaceNoInstall", { name: entry.name })}
            </div>
          </div>`
    }
    ${
      entry.install?.requiredEnv?.length
        ? html`<div class="card accent-brand">
            <div class="card-b" style="font-size:13px">
              ${t("mcp.marketplaceNeedsEnv", { names: entry.install.requiredEnv.join(", ") })}
            </div>
          </div>`
        : null
    }
  `;
}
