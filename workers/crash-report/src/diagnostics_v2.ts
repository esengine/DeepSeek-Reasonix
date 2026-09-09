import type { Env } from "./env";

export const DEVELOPMENT_FINGERPRINT_PREFIX = "dev:";
export const developmentGroupSQL = `groups.fingerprint LIKE 'dev:%'`;

type GroupPriorityRow = {
  fingerprint: string;
  status: string;
  severity: string;
  regressed_at: string;
  first_version: string;
  count: number;
  seen: string;
  title: string;
  last_version: string;
  last_channel: string;
  affected_installs?: number;
};

export function isDevelopmentGroup(row: Pick<GroupPriorityRow, "fingerprint">): boolean {
  return row.fingerprint.startsWith(DEVELOPMENT_FINGERPRINT_PREFIX);
}

export function effectiveGroupSeverity(
  row: Pick<GroupPriorityRow, "fingerprint" | "severity" | "title">,
): string {
  if (row.severity === "critical") return row.severity;
  if (isDevelopmentGroup(row)) return "low";
  if (
    row.title === "[window.error] Script error." ||
    row.title.includes("ResizeObserver loop ") ||
    row.title.includes("Minified React error #520") ||
    row.title.includes("additional File object is not a file on the disk")
  ) {
    return "low";
  }
  return row.severity;
}

function compareDiagnosticPriority(a: GroupPriorityRow, b: GroupPriorityRow, latestVersion: string): number {
  const statusRank = (value: string) => (value === "open" ? 0 : 1);
  const severityRank = (value: string) => ({ critical: 0, high: 1, medium: 2, low: 3 })[value] ?? 4;
  return (
    Number(b.affected_installs ?? 0) - Number(a.affected_installs ?? 0) ||
    statusRank(a.status) - statusRank(b.status) ||
    severityRank(a.severity) - severityRank(b.severity) ||
    Number(b.first_version === latestVersion) - Number(a.first_version === latestVersion) ||
    Number(Boolean(b.regressed_at)) - Number(Boolean(a.regressed_at)) ||
    b.count - a.count ||
    b.seen.localeCompare(a.seen)
  );
}

export function currentWindowSince(days: 7 | 30): string {
  return `-${days - 1} day`;
}

export function diagnosticWindowWhere(days: 7 | 30): string {
  return `date(last_seen) >= date('now', '${currentWindowSince(days)}')`;
}

export type DiagnosticBar = { label: string; users: number };

export type DiagnosticsQueryObserver = (label: string, durationMs: number, rows: number) => void;

export type DiagnosticFacets = {
  versions: DiagnosticBar[];
  platforms: DiagnosticBar[];
  osBuilds: DiagnosticBar[];
  osRevisions: DiagnosticBar[];
  distros: DiagnosticBar[];
  distroVersions: DiagnosticBar[];
  kernels: DiagnosticBar[];
  sessions: DiagnosticBar[];
  architectures: DiagnosticBar[];
  channels: DiagnosticBar[];
  runtimes: DiagnosticBar[];
  runtimeEngines: DiagnosticBar[];
  failureKinds: DiagnosticBar[];
  failureReasons: DiagnosticBar[];
  exitCodes: DiagnosticBar[];
  recoveries: DiagnosticBar[];
  gpuStates: DiagnosticBar[];
};

const DIAGNOSTIC_FACET_CACHE_MS = 30_000;
const diagnosticFacetCache = new WeakMap<object, Map<7 | 30, { expiresAt: number; value: DiagnosticFacets }>>();

async function observedAll<T>(
  statement: D1PreparedStatement,
  label: string,
  observe?: DiagnosticsQueryObserver,
): Promise<{ results: T[] }> {
  const started = performance.now();
  const result = await statement.all<T>();
  observe?.(label, performance.now() - started, result.results?.length ?? 0);
  return result;
}

async function observedFirst<T>(
  statement: D1PreparedStatement,
  label: string,
  observe?: DiagnosticsQueryObserver,
): Promise<T | null> {
  const started = performance.now();
  const result = await statement.first<T>();
  observe?.(label, performance.now() - started, result ? 1 : 0);
  return result;
}

export async function diagnosticFacets(
  env: Env,
  days: 7 | 30,
  observe?: DiagnosticsQueryObserver,
): Promise<DiagnosticFacets> {
  const cacheKey = env.DB as unknown as object;
  const cached = diagnosticFacetCache.get(cacheKey)?.get(days);
  if (cached && cached.expiresAt > Date.now()) {
    observe?.("diagnostic_facets.cache", 0, 17);
    return cached.value;
  }
  const since = currentWindowSince(days);
  const facet = (name: string, expression: string, extra = "") => observedAll<DiagnosticBar>(env.DB.prepare(
    `SELECT ${expression} AS label, COUNT(DISTINCT NULLIF(install_id, '')) AS users
     FROM report_event_dimensions WHERE date >= date('now', '${since}') ${extra}
     GROUP BY label ORDER BY users DESC LIMIT 20`,
  ), `diagnostic_facets.${name}`, observe).then((result) => result.results);
  const [versions, platforms, osBuilds, osRevisions, distros, distroVersions, kernels, sessions, architectures, channels, runtimes, runtimeEngines, failureKinds, failureReasons, exitCodes, recoveries, gpuStates] = await Promise.all([
    facet("version", "version", "AND version <> ''"),
    facet("platforms", "os", "AND os <> ''"),
    facet("os_builds", "CAST(os_build AS TEXT)", "AND os_build > 0"),
    facet("os_revisions", "CAST(os_revision AS TEXT)", "AND os_revision > 0"),
    facet("distros", "distro_id", "AND distro_id <> ''"),
    facet("distro_versions", "distro_version", "AND distro_version <> ''"),
    facet("kernels", "kernel_version", "AND kernel_version <> ''"),
    facet("sessions", "session_type", "AND session_type <> ''"),
    facet("architectures", "arch", "AND arch <> ''"),
    facet("channels", "channel", "AND channel <> ''"),
    facet("runtimes", "runtime_version", "AND runtime_version <> ''"),
    facet("runtime_engines", "runtime_engine", "AND runtime_engine <> ''"),
    facet("failure_kinds", "failure_kind", "AND failure_kind <> ''"),
    facet("failure_reasons", "failure_reason", "AND failure_reason <> ''"),
    facet("exit_codes", "exit_code", "AND exit_code <> ''"),
    facet("recoveries", "recovery", "AND recovery <> ''"),
    facet("gpu_states", "gpu_mode", "AND gpu_mode <> ''"),
  ]);
  const value = { versions, platforms, osBuilds, osRevisions, distros, distroVersions, kernels, sessions, architectures, channels, runtimes, runtimeEngines, failureKinds, failureReasons, exitCodes, recoveries, gpuStates };
  let byWindow = diagnosticFacetCache.get(cacheKey);
  if (!byWindow) {
    byWindow = new Map();
    diagnosticFacetCache.set(cacheKey, byWindow);
  }
  byWindow.set(days, { expiresAt: Date.now() + DIAGNOSTIC_FACET_CACHE_MS, value });
  return value;
}

type DiagnosticsGroupFilters = {
  status: string;
  source: string;
  version: string;
  os: string;
  platform: string;
  osBuild: string;
  osRevision?: string;
  distroId?: string;
  distroVersion?: string;
  kernelVersion?: string;
  sessionType?: string;
  arch: string;
  channel: string;
  runtimeVersion: string;
  runtimeEngine?: string;
  failureKind: string;
  failureReason: string;
  exitCode?: string;
  recovery: string;
  gpu: string;
  newLatest: boolean;
  regressed: boolean;
  windowDays: 7 | 30;
};

export async function crashGroups(
  env: Env,
  filters: DiagnosticsGroupFilters,
  latestVersion: string,
  observe?: DiagnosticsQueryObserver,
) {
  const where: string[] = [diagnosticWindowWhere(filters.windowDays)];
  const binds: unknown[] = [];
  const add = (sql: string, value?: unknown) => {
    where.push(sql);
    if (value !== undefined) binds.push(value);
  };
  if (filters.status) add("status = ?", filters.status);
  if (filters.source) add("source = ?", filters.source);
  const installWhere: string[] = [`date >= date('now', '${currentWindowSince(filters.windowDays)}')`];
  const installBinds: unknown[] = [];
  const addInstall = (column: string, value: unknown) => {
    installBinds.push(value);
    installWhere.push(value === null ? `${column} IS NULL` : `${column} = ?`);
    if (value === null) installBinds.pop();
  };
  if (filters.version) addInstall("version", filters.version);
  if (filters.os) addInstall("os", filters.os);
  if (filters.platform) addInstall("os", filters.platform);
  if (filters.osBuild) addInstall("os_build", Number(filters.osBuild));
  if (filters.osRevision) addInstall("os_revision", Number(filters.osRevision));
  if (filters.distroId) addInstall("distro_id", filters.distroId);
  if (filters.distroVersion) addInstall("distro_version", filters.distroVersion);
  if (filters.kernelVersion) addInstall("kernel_version", filters.kernelVersion);
  if (filters.sessionType) addInstall("session_type", filters.sessionType);
  if (filters.arch) addInstall("arch", filters.arch);
  if (filters.channel) addInstall("channel", filters.channel);
  if (filters.runtimeVersion) addInstall("runtime_version", filters.runtimeVersion);
  if (filters.runtimeEngine) addInstall("runtime_engine", filters.runtimeEngine);
  if (filters.failureKind) addInstall("failure_kind", filters.failureKind);
  if (filters.failureReason) addInstall("failure_reason", filters.failureReason);
  if (filters.exitCode) addInstall("exit_code", filters.exitCode);
  if (filters.recovery) addInstall("recovery", filters.recovery);
  if (filters.gpu) addInstall("gpu_mode", filters.gpu);
  if (installWhere.length > 1) where.push("COALESCE(diagnostics.window_events, 0) > 0");
  if (filters.newLatest && latestVersion) add("first_version = ?", latestVersion);
  if (filters.regressed) where.push("regressed_at <> ''");
  let latestOrder = "";
  if (latestVersion) {
    latestOrder = `CASE WHEN first_version = ? THEN 0 ELSE 1 END,`;
    binds.push(latestVersion);
  }
  const reportWindow = currentWindowSince(filters.windowDays);
  const pingWhere = [`date >= date('now', '${reportWindow}')`];
  const pingBinds: unknown[] = [];
  const pingBaseWhere = [`date >= date('now', '${reportWindow}')`];
  const pingBaseBinds: unknown[] = [];
  const dimensionKnown: string[] = [];
  const addPing = (column: string, value: unknown) => {
    pingWhere.push(`${column} = ?`);
    pingBinds.push(value);
  };
  const addPingBase = (column: string, value: unknown) => {
    pingBaseWhere.push(`${column} = ?`);
    pingBaseBinds.push(value);
    addPing(column, value);
  };
  if (filters.version) addPingBase("version", filters.version);
  if (filters.os) addPingBase("os", filters.os);
  if (filters.platform) addPingBase("os", filters.platform);
  if (!filters.os && !filters.platform && (filters.osBuild || filters.osRevision)) addPingBase("os", "windows");
  if (!filters.os && !filters.platform && (filters.distroId || filters.distroVersion || filters.kernelVersion || filters.sessionType)) addPingBase("os", "linux");
  if (filters.osBuild) { addPing("os_build", Number(filters.osBuild)); dimensionKnown.push("os_build > 0"); }
  if (filters.osRevision) { addPing("os_revision", Number(filters.osRevision)); dimensionKnown.push("os_revision > 0"); }
  if (filters.distroId) { addPing("distro_id", filters.distroId); dimensionKnown.push("distro_id <> ''"); }
  if (filters.distroVersion) { addPing("distro_version", filters.distroVersion); dimensionKnown.push("distro_version <> ''"); }
  if (filters.kernelVersion) { addPing("kernel_version", filters.kernelVersion); dimensionKnown.push("kernel_version <> ''"); }
  if (filters.sessionType) { addPing("session_type", filters.sessionType); dimensionKnown.push("session_type <> ''"); }
  if (filters.arch) addPingBase("arch", filters.arch);
  if (filters.channel) { addPing("channel", filters.channel); dimensionKnown.push("channel <> ''"); }
  if (filters.runtimeVersion) { addPing("runtime_version", filters.runtimeVersion); dimensionKnown.push("runtime_version <> ''"); }
  if (filters.runtimeEngine) { addPing("runtime_engine", filters.runtimeEngine); dimensionKnown.push("runtime_engine <> ''"); }
  if (filters.gpu) { addPing("gpu_mode", filters.gpu); dimensionKnown.push("gpu_mode <> ''"); }
  const activeInstalls = `(SELECT COUNT(DISTINCT install_id) FROM pings WHERE ${pingWhere.join(" AND ")})`;
  const baseInstalls = `(SELECT COUNT(DISTINCT install_id) FROM pings WHERE ${pingBaseWhere.join(" AND ")})`;
  const coveredInstalls = `(SELECT COUNT(DISTINCT install_id) FROM pings WHERE ${[...pingBaseWhere, ...dimensionKnown].join(" AND ")})`;
  const samePingWindow = pingWhere.length === 1 && pingBaseWhere.length === 1 && dimensionKnown.length === 0;
  const pingStatsJoin = samePingWindow
    ? `CROSS JOIN (SELECT COUNT(DISTINCT install_id) AS installs FROM pings WHERE ${pingWhere.join(" AND ")}) ping_stats`
    : "";
  const activeInstallExpr = samePingWindow ? "ping_stats.installs" : activeInstalls;
  const baseInstallExpr = samePingWindow ? "ping_stats.installs" : baseInstalls;
  const coveredInstallExpr = samePingWindow ? "ping_stats.installs" : coveredInstalls;
  const diagnosticJoin = installWhere.length > 1
    ? `LEFT JOIN (
      SELECT fingerprint,
        COUNT(DISTINCT NULLIF(install_id, '')) AS affected_installs,
        SUM(events) AS window_events,
        SUM(CASE WHEN install_id <> '' THEN events ELSE 0 END) AS identified_events
      FROM report_event_dimensions WHERE ${installWhere.join(" AND ")} GROUP BY fingerprint
    ) diagnostics ON diagnostics.fingerprint = groups.fingerprint`
    : `LEFT JOIN (
      SELECT daily.fingerprint,
        COALESCE(installs.affected_installs, 0) AS affected_installs,
        SUM(daily.events) AS window_events,
        SUM(daily.identified_events) AS identified_events
      FROM report_daily AS daily
      LEFT JOIN (
        SELECT fingerprint, COUNT(DISTINCT install_id) AS affected_installs
        FROM report_installations WHERE date >= date('now', '${currentWindowSince(filters.windowDays)}')
        GROUP BY fingerprint
      ) installs ON installs.fingerprint = daily.fingerprint
      WHERE daily.date >= date('now', '${currentWindowSince(filters.windowDays)}')
      GROUP BY daily.fingerprint, installs.affected_installs
    ) diagnostics ON diagnostics.fingerprint = groups.fingerprint`;
  const sql = `SELECT groups.fingerprint, kind, count, first_version, last_version, substr(last_seen, 1, 10) AS seen,
      status, title, source, label, error_type, top_frame, severity, last_os, last_arch, last_channel, regressed_at,
      COALESCE(diagnostics.affected_installs, 0) AS affected_installs,
      COALESCE(diagnostics.window_events, 0) AS window_events,
      COALESCE(diagnostics.identified_events, 0) AS identified_events,
      ${activeInstallExpr} AS active_build_installs,
      ${baseInstallExpr} AS dimension_base_installs,
      ${coveredInstallExpr} AS dimension_covered_installs
    FROM groups
    ${diagnosticJoin}
    ${pingStatsJoin}
    ${where.length ? `WHERE ${where.join(" AND ")}` : ""}
    ORDER BY
      affected_installs DESC,
      window_events DESC,
      CASE WHEN status = 'open' THEN 0 ELSE 1 END,
      CASE
        WHEN severity = 'critical' THEN 0
        WHEN ${developmentGroupSQL}
          OR title = '[window.error] Script error.'
          OR title LIKE '%ResizeObserver loop %'
          OR title LIKE '%Minified React error #520%'
          OR title LIKE '%additional File object is not a file on the disk%'
          THEN 3
        WHEN severity = 'high' THEN 1
        WHEN severity = 'medium' THEN 2
        ELSE 3
      END,
      ${latestOrder}
      CASE WHEN regressed_at <> '' THEN 0 ELSE 1 END,
      count DESC,
      last_seen DESC
    LIMIT 50`;
  const allBinds = [...pingBinds, ...pingBaseBinds, ...pingBaseBinds, ...installBinds, ...binds];
  const stmt = env.DB.prepare(sql);
  const query = allBinds.length ? stmt.bind(...allBinds) : stmt;
  const started = performance.now();
  const result = await query.all<GroupPriorityRow & {
    kind: string;
    source: string;
    label: string;
    error_type: string;
    top_frame: string;
    last_os: string;
    last_arch: string;
    window_events: number;
    identified_events: number;
    active_build_installs: number;
    dimension_base_installs: number;
    dimension_covered_installs: number;
  }>();
  observe?.("diagnostic_groups", performance.now() - started, result.results?.length ?? 0);
  result.results = result.results
    .map((row) => ({
      ...row,
      severity: effectiveGroupSeverity(row),
      development: isDevelopmentGroup(row),
      identity_coverage: row.window_events ? row.identified_events / row.window_events : 0,
      dimension_coverage: row.dimension_base_installs
        ? row.dimension_covered_installs / row.dimension_base_installs
        : dimensionKnown.length ? 0 : 1,
      impact_rate: row.active_build_installs ? Number(row.affected_installs ?? 0) / row.active_build_installs : null,
    }))
    .sort((a, b) => compareDiagnosticPriority(a, b, latestVersion));
  return result;
}

type ReportAggregateInput = {
  installId?: string;
  version: string;
  os: string;
  arch: string;
  device?: {
    osBuild?: number;
    osRevision?: number;
    distroId?: string;
    distroVersion?: string;
    kernelVersion?: string;
    sessionType?: string;
  };
};

type WebRuntimeAggregateInput = {
  engine: string;
  runtimeVersion: string;
  kind: string;
  reason: string;
  exitCode?: number;
  recovery: string;
  gpuMode: string;
};

export function reportAggregateStatements(
  db: Env["DB"],
  report: ReportAggregateInput,
  fingerprint: string,
  channel: string,
  webRuntime?: WebRuntimeAggregateInput,
): D1PreparedStatement[] {
  const statements = [
    db.prepare(
      `INSERT INTO report_daily (date, fingerprint, events, identified_events)
       VALUES (date('now'), ?1, 1, ?2)
       ON CONFLICT (date, fingerprint) DO UPDATE SET
         events = events + 1, identified_events = identified_events + ?2`,
    ).bind(fingerprint, report.installId ? 1 : 0),
  ];
  if (report.installId) {
    statements.push(
      db.prepare(
        `INSERT INTO report_installations (
           date, fingerprint, install_id, version, os, arch, os_build, os_revision,
           distro_id, distro_version, kernel_version, session_type, channel,
           runtime_engine, runtime_version, failure_kind, failure_reason, exit_code, recovery, gpu_mode, events
         ) VALUES (date('now'), ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, 1)
         ON CONFLICT (date, fingerprint, install_id) DO UPDATE SET
           version = ?3, os = ?4, arch = ?5, os_build = ?6, os_revision = ?7,
           distro_id = ?8, distro_version = ?9, kernel_version = ?10, session_type = ?11, channel = ?12,
           runtime_engine = ?13, runtime_version = ?14, failure_kind = ?15, failure_reason = ?16,
           exit_code = ?17, recovery = ?18, gpu_mode = ?19, events = events + 1`,
      ).bind(
        fingerprint, report.installId, report.version, report.os, report.arch,
        report.device?.osBuild ?? 0, report.device?.osRevision ?? 0,
        report.device?.distroId ?? "", report.device?.distroVersion ?? "", report.device?.kernelVersion ?? "",
        report.device?.sessionType ?? "", channel,
        webRuntime?.engine ?? "", webRuntime?.runtimeVersion ?? "", webRuntime?.kind ?? "", webRuntime?.reason ?? "",
        webRuntime?.exitCode ?? null, webRuntime?.recovery ?? "", webRuntime?.gpuMode ?? "unknown",
      ),
    );
  }
  statements.push(
    db.prepare(
      `INSERT INTO report_event_dimensions (
         date, fingerprint, install_id, version, os, arch, os_build, os_revision,
         distro_id, distro_version, kernel_version, session_type, channel,
         runtime_engine, runtime_version, failure_kind, failure_reason, exit_code, recovery, gpu_mode, events
       ) VALUES (date('now'), ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, 1)
       ON CONFLICT (
         date, fingerprint, install_id, version, os, arch, os_build, os_revision,
         distro_id, distro_version, kernel_version, session_type, channel,
         runtime_engine, runtime_version, failure_kind, failure_reason, exit_code, recovery, gpu_mode
       ) DO UPDATE SET events = events + 1`,
    ).bind(
      fingerprint, report.installId ?? "", report.version, report.os, report.arch,
      report.device?.osBuild ?? 0, report.device?.osRevision ?? 0,
      report.device?.distroId ?? "", report.device?.distroVersion ?? "", report.device?.kernelVersion ?? "",
      report.device?.sessionType ?? "", channel,
      webRuntime?.engine ?? "", webRuntime?.runtimeVersion ?? "", webRuntime?.kind ?? "", webRuntime?.reason ?? "",
      webRuntime?.exitCode === undefined ? "unknown" : String(webRuntime.exitCode), webRuntime?.recovery ?? "",
      webRuntime?.gpuMode ?? "unknown",
    ),
  );
  return statements;
}

export type GroupDiagnosticSummary = {
  windowEvents: number;
  identifiedEvents: number;
  affectedInstalls: number;
  distributions: { facet: string; value: string; installs: number; events: number }[];
};

export async function groupDiagnosticSummary(
  env: Env,
  fingerprint: string,
  observe?: DiagnosticsQueryObserver,
): Promise<GroupDiagnosticSummary> {
  const [totals, distributions] = await Promise.all([
    observedFirst<{ window_events: number; identified_events: number; affected_installs: number }>(env.DB.prepare(
      `SELECT
         COALESCE(daily.window_events, 0) AS window_events,
         COALESCE(daily.identified_events, 0) AS identified_events,
         COALESCE(installs.affected_installs, 0) AS affected_installs
       FROM (
         SELECT SUM(events) AS window_events, SUM(identified_events) AS identified_events
         FROM report_daily WHERE fingerprint = ?1 AND date >= date('now', '-29 day')
       ) daily
       CROSS JOIN (
         SELECT COUNT(DISTINCT install_id) AS affected_installs
         FROM report_installations WHERE fingerprint = ?1 AND date >= date('now', '-29 day')
       ) installs`,
    ).bind(fingerprint), "group_diagnostic_totals", observe),
    observedAll<{ facet: string; value: string; installs: number; events: number }>(env.DB.prepare(
      `WITH window AS MATERIALIZED (
         SELECT os, arch, os_build, os_revision, runtime_version, runtime_engine,
                distro_id, distro_version, kernel_version, session_type,
                failure_kind, failure_reason, exit_code, gpu_mode, recovery,
                install_id, events
         FROM report_event_dimensions
         WHERE fingerprint = ?1 AND date >= date('now', '-29 day')
       ), facts AS (
         SELECT 'platform' AS facet, os || ' ' || arch AS value, install_id, events FROM window WHERE os <> ''
         UNION ALL SELECT 'osBuild', CAST(os_build AS TEXT), install_id, events FROM window WHERE os_build > 0
         UNION ALL SELECT 'osRevision', CAST(os_revision AS TEXT), install_id, events FROM window WHERE os_revision > 0
         UNION ALL SELECT 'runtime', runtime_version, install_id, events FROM window WHERE runtime_version <> ''
         UNION ALL SELECT 'runtimeEngine', runtime_engine, install_id, events FROM window WHERE runtime_engine <> ''
         UNION ALL SELECT 'distro', distro_id || ' ' || distro_version, install_id, events FROM window WHERE distro_id <> ''
         UNION ALL SELECT 'kernel', kernel_version, install_id, events FROM window WHERE kernel_version <> ''
         UNION ALL SELECT 'session', session_type, install_id, events FROM window WHERE session_type <> ''
         UNION ALL SELECT 'kind', failure_kind, install_id, events FROM window WHERE failure_kind <> ''
         UNION ALL SELECT 'reason', failure_reason, install_id, events FROM window WHERE failure_reason <> ''
         UNION ALL SELECT 'exitCode', exit_code, install_id, events FROM window
         UNION ALL SELECT 'gpu', gpu_mode, install_id, events FROM window
         UNION ALL SELECT 'recovery', recovery, install_id, events FROM window WHERE recovery <> ''
       ), grouped AS (
         SELECT facet, value, COUNT(DISTINCT NULLIF(install_id, '')) AS installs, SUM(events) AS events
         FROM facts GROUP BY facet, value
       ), ranked AS (
         SELECT facet, value, installs, events,
                ROW_NUMBER() OVER (PARTITION BY facet ORDER BY installs DESC, events DESC, value) AS rank
         FROM grouped
       )
       SELECT facet, value, installs, events
       FROM ranked WHERE rank <= 20 ORDER BY facet, rank`,
    ).bind(fingerprint), "group_diagnostic_distributions", observe),
  ]);
  return {
    windowEvents: Number(totals?.window_events ?? 0),
    identifiedEvents: Number(totals?.identified_events ?? 0),
    affectedInstalls: Number(totals?.affected_installs ?? 0),
    distributions: distributions.results,
  };
}
