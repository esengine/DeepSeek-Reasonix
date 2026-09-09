import type { Env } from "./env";
import type { ReportSample } from "./group";
import {
  groupDiagnosticSummary,
  type DiagnosticsQueryObserver,
  type GroupDiagnosticSummary,
} from "./diagnostics_v2";

const REPORT_COLUMNS = `
  id, version, os, arch, message, device, created_at, source, label, error_type, error_message,
  top_frame, build_commit, channel, language, view, breadcrumbs, component_stack, stack,
  occurred_at, webview2, web_runtime`;

// Keep the detail page bounded even when a group has a large lifetime count.
// The first retained sample preserves the earliest context; the latest samples
// show the current failure shape. UNION removes the duplicate when a group has
// fewer rows than the requested latest sample window.
export function groupReportsStatement(db: D1Database, fingerprint: string, latestSamples: number): D1PreparedStatement {
  return db.prepare(
    `WITH first_sample AS (
       SELECT ${REPORT_COLUMNS}
       FROM reports INDEXED BY reports_fingerprint_id WHERE fingerprint = ?1 ORDER BY id ASC LIMIT 1
     ), latest_samples AS (
       SELECT ${REPORT_COLUMNS}
       FROM reports INDEXED BY reports_fingerprint_id WHERE fingerprint = ?1 ORDER BY id DESC LIMIT ?2
     ), retained AS (
       SELECT * FROM first_sample
       UNION
       SELECT * FROM latest_samples
     )
     SELECT ${REPORT_COLUMNS} FROM retained ORDER BY id DESC`,
  ).bind(fingerprint, latestSamples);
}

export async function loadD1GroupReports(
  env: Env,
  fingerprint: string,
  latestSamples: number,
  observe: DiagnosticsQueryObserver,
): Promise<{ reports: ReportSample[]; unavailable: boolean }> {
  try {
    const started = performance.now();
    const stored = await groupReportsStatement(env.DB, fingerprint, latestSamples).all<ReportSample>();
    observe("group_samples", performance.now() - started, stored.results?.length ?? 0);
    return { reports: stored.results, unavailable: false };
  } catch (error) {
    console.error("group raw sample query failed", error);
    return { reports: [], unavailable: true };
  }
}

export async function loadGroupDiagnostics(
  env: Env,
  fingerprint: string,
  observe: DiagnosticsQueryObserver,
): Promise<{ summary?: GroupDiagnosticSummary; unavailable: boolean }> {
  try {
    return { summary: await groupDiagnosticSummary(env, fingerprint, observe), unavailable: false };
  } catch (error) {
    console.error("group diagnostic summary query failed", error);
    return { unavailable: true };
  }
}
