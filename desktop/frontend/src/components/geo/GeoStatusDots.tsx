import { createContext, useContext, useState, type ReactNode } from "react";
import gdalSvg from "../../assets/icons/geo/gdal.svg";
import qgisSvg from "../../assets/icons/geo/qgis.svg";
import geeSvg from "../../assets/icons/geo/gee.svg";
import earthSvg from "../../assets/icons/geo/earth.svg";

export interface GeoComponentStatus {
  gdal: "ready" | "raster-only" | "vector-only" | "bad" | "unknown";
  qgis: "ready" | "bad" | "not-installed" | "unknown";
  gee: "ready" | "auth-required" | "not-installed" | "init-failed" | "unknown";
}

const DOT_COLORS: Record<string, string> = {
  ready: "#22c55e",
  "raster-only": "#eab308",
  "vector-only": "#eab308",
  bad: "#ef4444",
  "not-installed": "#ef4444",
  "auth-required": "#eab308",
  "init-failed": "#ef4444",
  unknown: "#6b7280",
};

const STATUS_LABELS: Record<string, string> = {
  ready: "OK",
  "raster-only": "raster only",
  "vector-only": "vector only",
  bad: "error",
  "not-installed": "not found",
  "auth-required": "auth needed",
  "init-failed": "init failed",
  unknown: "untested",
};

const ENV_ICONS = {
  gdal: gdalSvg,
  qgis: qgisSvg,
  gee: geeSvg,
};

export const GEO_TOOL_ICONS: Record<string, string> = {
  "mcp__geocode__read_geo_data": earthSvg,
  "mcp__geocode__run_qgis_algorithm": qgisSvg,
  "mcp__geocode__qgis_doc": qgisSvg,
  "mcp__geocode__run_gee_script": geeSvg,
  "mcp__geocode__geo_env_status": gdalSvg,
};

// ── Context ──────────────────────────────────────────────────

interface GeoStatusContextValue {
  status: GeoComponentStatus;
  setStatus: (s: GeoComponentStatus) => void;
}

const GeoStatusContext = createContext<GeoStatusContextValue>({
  status: { gdal: "unknown", qgis: "unknown", gee: "unknown" },
  setStatus: () => {},
});

export function GeoStatusProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<GeoComponentStatus>({
    gdal: "unknown",
    qgis: "unknown",
    gee: "unknown",
  });
  return (
    <GeoStatusContext.Provider value={{ status, setStatus }}>
      {children}
    </GeoStatusContext.Provider>
  );
}

export function useGeoStatus() {
  return useContext(GeoStatusContext);
}

// ── Single dot + icon button ─────────────────────────────────

function EnvDot({
  env,
  state,
  details,
}: {
  env: "gdal" | "qgis" | "gee";
  state: string;
  details: Record<string, unknown> | null;
}) {
  const color = DOT_COLORS[state] ?? DOT_COLORS.unknown;
  const label = STATUS_LABELS[state] ?? state;
  const [open, setOpen] = useState(false);

  return (
    <span className="geodot">
      <button className="geodot__btn" onClick={() => setOpen((v) => !v)} title={`${env.toUpperCase()}: ${label}`}>
        <img src={ENV_ICONS[env]} alt={env} className="geodot__icon" />
        <span className="geodot__badge" style={{ backgroundColor: color }} />
      </button>
      {open && (
        <>
          <div className="geodot__backdrop" onClick={() => setOpen(false)} />
          <div className="geodot__popover">
            <div className="geodot__pophead">
              <img src={ENV_ICONS[env]} alt={env} className="geodot__popicon" />
              <span>{env.toUpperCase()}</span>
              <span className="geodot__popstatus" style={{ color }}>{label}</span>
            </div>
            {details ? (
              <div className="geodot__popbody">
                {Object.entries(details).map(([k, v]) =>
                  v != null ? (
                    <div key={k} className="geodot__poprow">
                      <span className="geodot__popkey">{k}</span>
                      <span className="geodot__popval">{String(v)}</span>
                    </div>
                  ) : null,
                )}
              </div>
            ) : (
              <div className="geodot__popbody">
                <span className="geodot__popkey">Run <code>mcp__geocode__geo_env_status</code> to probe.</span>
              </div>
            )}
          </div>
        </>
      )}
    </span>
  );
}

// ── Three dots ───────────────────────────────────────────────

export function GeoStatusDots() {
  const { status } = useGeoStatus();

  return (
    <span className="geodots">
      <EnvDot env="gdal" state={status.gdal} details={null} />
      <EnvDot env="qgis" state={status.qgis} details={null} />
      <EnvDot env="gee" state={status.gee} details={null} />
    </span>
  );
}

// ── Parse geo_env_status output ──────────────────────────────

export function parseEnvStatus(output: string): GeoComponentStatus | null {
  try {
    const obj = JSON.parse(output);
    if (!obj.__env_block__) return null;
    return {
      gdal: obj.gdal?.status ?? "unknown",
      qgis: obj.qgis?.status ?? "unknown",
      gee: obj.gee?.status ?? "unknown",
    };
  } catch {
    return null;
  }
}
