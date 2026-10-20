import { t } from "../i18n";
import "../styles/production-manifest.css";

export const isProductionManifest = (path: string) => /(^|\/)video\/[^/]+\/manifest\.json$/.test(path);

type Artifact = { artifact_id?: string; version?: number; shot_id?: string; path?: string; sha256?: string; superseded_by?: number };
type Review = { artifact_id?: string; version?: number; sha256?: string; brief_revision?: number; scope?: string; verdict?: string };
type Shot = { shot_id?: string; start?: number; end?: number; prompt_revision?: number };
type Manifest = { version?: number; production_id?: string; revision?: number; brief?: { revision?: number }; shots?: Shot[]; artifacts?: Artifact[]; reviews?: Review[]; delivery?: { artifact_id?: string; version?: number } | null };

export type ShotRow = { shotID: string; range: string; promptRevision?: number; versions: { version: number; path: string; current: boolean; reviews: { scope: string; verdict: string; stale: boolean }[] }[] };

export function parseManifest(text: string): { rows: ShotRow[]; production: string; revision?: number; delivered?: string } | { unsupported: string } {
  let doc: Manifest;
  try {
    doc = JSON.parse(text) as Manifest;
  } catch {
    return { unsupported: t("清单不是有效的 JSON") };
  }
  if (doc.version !== 1) return { unsupported: t("不支持的清单版本：{v}", { v: String(doc.version) }) };
  const briefRevision = doc.brief?.revision;
  const rows = (doc.shots ?? []).filter((s) => s.shot_id).map((s) => {
    const versions = (doc.artifacts ?? [])
      .filter((a) => a.shot_id === s.shot_id && typeof a.version === "number" && a.path)
      .sort((a, b) => (a.version ?? 0) - (b.version ?? 0))
      .map((a) => ({
        version: a.version!,
        path: a.path!,
        current: a.superseded_by === undefined,
        reviews: (doc.reviews ?? [])
          .filter((r) => r.artifact_id === a.artifact_id && r.version === a.version && r.scope && r.verdict)
          .map((r) => ({ scope: r.scope!, verdict: r.verdict!, stale: r.sha256 !== a.sha256 || r.brief_revision !== briefRevision })),
      }));
    const range = typeof s.start === "number" && typeof s.end === "number" ? `${s.start}–${s.end}s` : "";
    return { shotID: s.shot_id!, range, promptRevision: s.prompt_revision, versions };
  });
  const d = doc.delivery;
  return { rows, production: doc.production_id ?? "", revision: doc.revision, delivered: d?.artifact_id ? `${d.artifact_id} v${d.version}` : undefined };
}

export function ProductionManifest({ text, onOpen }: { text: string; onOpen: (path: string) => void }) {
  const parsed = parseManifest(text);
  if ("unsupported" in parsed) return <div className="workbench-error" role="alert">{parsed.unsupported}</div>;
  return (
    <div className="production-manifest">
      <header>
        <strong>{parsed.production}</strong>
        {parsed.revision !== undefined && <span>{t("修订 {n}", { n: String(parsed.revision) })}</span>}
        <span>{parsed.delivered ? t("已交付：{a}", { a: parsed.delivered }) : t("尚未交付")}</span>
      </header>
      <table>
        <thead><tr><th>{t("镜头")}</th><th>{t("时间")}</th><th>{t("版本与验收")}</th></tr></thead>
        <tbody>
          {parsed.rows.map((row) => (
            <tr key={row.shotID}>
              <td>{row.shotID}{row.promptRevision !== undefined && <small> p{row.promptRevision}</small>}</td>
              <td>{row.range}</td>
              <td>
                {row.versions.length === 0 && <span className="production-pending">{t("尚无产物")}</span>}
                {row.versions.map((v) => (
                  <div key={v.version} className={v.current ? "production-version current" : "production-version"}>
                    <button type="button" data-action-click="workbench.file" data-target={v.path} onClick={() => onOpen(v.path)} title={v.path}>v{v.version}</button>
                    {v.reviews.map((r) => (
                      <span key={r.scope} className={`production-verdict ${r.stale ? "stale" : r.verdict}`} title={r.stale ? t("已过期：文件或需求已变") : r.verdict}>
                        {r.scope}
                      </span>
                    ))}
                  </div>
                ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
