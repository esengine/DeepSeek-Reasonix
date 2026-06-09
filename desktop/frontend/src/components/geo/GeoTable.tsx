import { useCallback, useEffect, useMemo, useState } from "react";

interface GeoTableProps {
  geojsonUrl: string;
  featureCount: number;
  className?: string;
}

const PAGE_SIZE = 100;

function guessColumns(features: Record<string, unknown>[]): string[] {
  const seen = new Set<string>();
  for (const f of features) {
    const props = (f.properties as Record<string, unknown>) ?? {};
    for (const key of Object.keys(props)) {
      if (seen.size < 30) seen.add(key);
    }
  }
  return [...seen];
}

export function GeoTable({ geojsonUrl, featureCount, className }: GeoTableProps) {
  const [features, setFeatures] = useState<Record<string, unknown>[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [sortCol, setSortCol] = useState<string | null>(null);
  const [sortAsc, setSortAsc] = useState(true);

  useEffect(() => {
    setLoading(true);
    setError(null);
    fetch(geojsonUrl)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((geojson) => {
        const feats = (geojson.features as Record<string, unknown>[]) ?? [];
        setFeatures(feats);
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message);
        setLoading(false);
      });
  }, [geojsonUrl]);

  const columns = useMemo(() => guessColumns(features), [features]);

  const sorted = useMemo(() => {
    if (!sortCol) return features;
    return [...features].sort((a, b) => {
      const pa = (a.properties as Record<string, unknown>)?.[sortCol];
      const pb = (b.properties as Record<string, unknown>)?.[sortCol];
      const sa = pa == null ? "" : String(pa);
      const sb = pb == null ? "" : String(pb);
      return sortAsc ? sa.localeCompare(sb) : sb.localeCompare(sa);
    });
  }, [features, sortCol, sortAsc]);

  const totalPages = Math.ceil(sorted.length / PAGE_SIZE);
  const pageFeatures = sorted.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);

  const toggleSort = useCallback(
    (col: string) => {
      if (sortCol === col) {
        setSortAsc((v) => !v);
      } else {
        setSortCol(col);
        setSortAsc(true);
      }
    },
    [sortCol],
  );

  if (loading) {
    return <div className="geotable__loading">Loading {featureCount} features...</div>;
  }

  if (error) {
    return <div className="geotable__error">Failed to load attribute table: {error}</div>;
  }

  return (
    <div className={`geotable ${className ?? ""}`}>
      <div className="geotable__head">
        <span>
          Attributes ({sorted.length} features · {columns.length} fields)
        </span>
        {totalPages > 1 && (
          <span className="geotable__pager">
            <button disabled={page <= 0} onClick={() => setPage((p) => p - 1)}>
              ←
            </button>
            <span>
              {page + 1} / {totalPages}
            </span>
            <button disabled={page >= totalPages - 1} onClick={() => setPage((p) => p + 1)}>
              →
            </button>
          </span>
        )}
      </div>
      <div className="geotable__wrap">
        <table className="geotable__table">
          <thead>
            <tr>
              <th className="geotable__fid">#</th>
              {columns.map((col) => (
                <th key={col} onClick={() => toggleSort(col)} className="geotable__col">
                  {col}
                  {sortCol === col && (
                    <span className="geotable__sort">{sortAsc ? " ▲" : " ▼"}</span>
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {pageFeatures.map((f, i) => {
              const props = (f.properties as Record<string, unknown>) ?? {};
              return (
                <tr key={i}>
                  <td className="geotable__fid">{page * PAGE_SIZE + i + 1}</td>
                  {columns.map((col) => (
                    <td key={col}>{String(props[col] ?? "")}</td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
