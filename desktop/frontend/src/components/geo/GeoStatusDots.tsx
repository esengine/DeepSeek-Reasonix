import { createContext, useContext, useState, type ReactNode } from "react";

export interface GeoComponentStatus {
  gdal: "ready" | "raster-only" | "vector-only" | "bad" | "unknown";
  qgis: "ready" | "bad" | "not-installed" | "unknown";
  gee: "ready" | "auth-required" | "not-installed" | "init-failed" | "unknown";
}

const STATUS_COLORS: Record<string, string> = {
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
  "auth-required": "auth",
  "init-failed": "init failed",
  unknown: "untested",
};

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

function Dot({ label, state }: { label: string; state: string }) {
  const color = STATUS_COLORS[state] ?? STATUS_COLORS.unknown;
  const text = STATUS_LABELS[state] ?? state;
  return (
    <span className="geodots__dot" title={`${label}: ${text}`}>
      <span
        className="geodots__circle"
        style={{ backgroundColor: color }}
      />
    </span>
  );
}

export function GeoStatusDots() {
  const { status } = useGeoStatus();
  return (
    <span className="geodots">
      <Dot label="GDAL" state={status.gdal} />
      <Dot label="QGIS" state={status.qgis} />
      <Dot label="GEE" state={status.gee} />
    </span>
  );
}

/** Parse geo_env_status output into GeoComponentStatus.
 *  Called from ToolCard when a geo_env_status result arrives. */
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
