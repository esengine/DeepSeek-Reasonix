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

export interface GeoComponentDetail {
  version?: string;
  path?: string;
  project?: string;
  processingReady?: boolean;
  error?: string;
}

export interface GeoDetails {
  gdal: GeoComponentDetail | null;
  qgis: GeoComponentDetail | null;
  gee: GeoComponentDetail | null;
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

const DOT_LABELS: Record<string, string> = {
  ready: "OK",
  "raster-only": "raster only",
  "vector-only": "vector only",
  bad: "error",
  "not-installed": "not found",
  "auth-required": "auth needed",
  "init-failed": "init failed",
  unknown: "untested",
};

const ENV_LABELS = { gdal: "GDAL", qgis: "QGIS", gee: "GEE" };

const ENV_ICONS: Record<string, string> = {
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
  details: GeoDetails;
  setStatus: (s: GeoComponentStatus) => void;
  setDetails: (d: GeoDetails) => void;
}

const defaultStatus: GeoComponentStatus = { gdal: "unknown", qgis: "unknown", gee: "unknown" };
const defaultDetails: GeoDetails = { gdal: null, qgis: null, gee: null };

const GeoStatusContext = createContext<GeoStatusContextValue>({
  status: defaultStatus,
  details: defaultDetails,
  setStatus: () => {},
  setDetails: () => {},
});

export function GeoStatusProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<GeoComponentStatus>(defaultStatus);
  const [details, setDetails] = useState<GeoDetails>(defaultDetails);
  return (
    <GeoStatusContext.Provider value={{ status, details, setStatus, setDetails }}>
      {children}
    </GeoStatusContext.Provider>
  );
}

export function useGeoStatus() {
  return useContext(GeoStatusContext);
}

// ── Single env icon + badge + popover ────────────────────────

function EnvPopover({
  env,
  state,
  detail,
  onClose,
}: {
  env: "gdal" | "qgis" | "gee";
  state: string;
  detail: GeoComponentDetail | null;
  onClose: () => void;
}) {
  const color = DOT_COLORS[state] ?? DOT_COLORS.unknown;
  const label = DOT_LABELS[state] ?? state;

  return (
    <>
      <div className="geodot__backdrop" onClick={onClose} />
      <div className="geodot__popover">
        <div className="geodot__pophead">
          <img src={ENV_ICONS[env]} alt={env} className="geodot__popicon" />
          <span>{ENV_LABELS[env]}</span>
          <span className="geodot__popstatus" style={{ color }}>{label}</span>
        </div>
        <div className="geodot__popbody">
          {detail ? (
            <>
              {detail.version && (
                <div className="geodot__poprow">
                  <span className="geodot__popkey">Version</span>
                  <span className="geodot__popval">{detail.version}</span>
                </div>
              )}
              {detail.path && (
                <div className="geodot__poprow">
                  <span className="geodot__popkey">Path</span>
                  <span className="geodot__popval" title={detail.path}>{truncatePath(detail.path)}</span>
                </div>
              )}
              {detail.project && (
                <div className="geodot__poprow">
                  <span className="geodot__popkey">Project</span>
                  <span className="geodot__popval">{detail.project}</span>
                </div>
              )}
              {detail.processingReady !== undefined && (
                <div className="geodot__poprow">
                  <span className="geodot__popkey">Processing</span>
                  <span className="geodot__popval" style={{ color: detail.processingReady ? "#22c55e" : "#ef4444" }}>
                    {detail.processingReady ? "ready" : "not ready"}
                  </span>
                </div>
              )}
              {detail.error && (
                <div className="geodot__poprow">
                  <span className="geodot__popkey">Error</span>
                  <span className="geodot__popval" style={{ color: "#ef4444" }}>{detail.error}</span>
                </div>
              )}
            </>
          ) : (
            <span className="geodot__popkey">Run <code>geo_env_status</code> to probe.</span>
          )}
        </div>
      </div>
    </>
  );
}

function truncatePath(p: string): string {
  if (p.length <= 40) return p;
  return "…" + p.slice(-38);
}

function EnvDot({ env }: { env: "gdal" | "qgis" | "gee" }) {
  const { status, details } = useGeoStatus();
  const state = status[env];
  const detail = details[env];
  const color = DOT_COLORS[state] ?? DOT_COLORS.unknown;
  const label = DOT_LABELS[state] ?? state;
  const [open, setOpen] = useState(false);

  return (
    <span className="geodot">
      <button
        className="geodot__btn"
        onClick={() => setOpen((v) => !v)}
        title={`${ENV_LABELS[env]}: ${label}`}
      >
        <img src={ENV_ICONS[env]} alt={env} className="geodot__icon" />
        <span className="geodot__badge" style={{ backgroundColor: color }} />
      </button>
      {open && (
        <EnvPopover env={env} state={state} detail={detail} onClose={() => setOpen(false)} />
      )}
    </span>
  );
}

// ── Three dots ───────────────────────────────────────────────

export function GeoStatusDots() {
  return (
    <span className="geodots">
      <EnvDot env="gdal" />
      <EnvDot env="qgis" />
      <EnvDot env="gee" />
    </span>
  );
}

// ── Parse geo_env_status output ──────────────────────────────

export function parseEnvStatus(output: string): {
  status: GeoComponentStatus;
  details: GeoDetails;
} | null {
  try {
    const obj = JSON.parse(output);
    if (!obj.__env_block__) return null;
    return {
      status: {
        gdal: obj.gdal?.status ?? "unknown",
        qgis: obj.qgis?.status ?? "unknown",
        gee: obj.gee?.status ?? "unknown",
      },
      details: {
        gdal: { version: obj.gdal?.version, path: obj.gdal?.bin_dir },
        qgis: { version: obj.qgis?.version, path: obj.qgis?.python, processingReady: obj.qgis?.processing_ready },
        gee: { version: obj.gee?.version, project: obj.gee?.project },
      },
    };
  } catch {
    return null;
  }
}
