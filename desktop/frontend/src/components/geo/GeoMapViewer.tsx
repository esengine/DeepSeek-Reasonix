import { useEffect, useRef, useState } from "react";
import Map from "ol/Map";
import View from "ol/View";
import TileLayer from "ol/layer/Tile";
import ImageLayer from "ol/layer/Image";
import VectorLayer from "ol/layer/Vector";
import VectorSource from "ol/source/Vector";
import ImageStatic from "ol/source/ImageStatic";
import OSM from "ol/source/OSM";
import XYZ from "ol/source/XYZ";
import GeoJSON from "ol/format/GeoJSON";
import { fromLonLat } from "ol/proj";
import "ol/ol.css";

export interface RasterPreview {
  __geo_type__: "raster_preview";
  preview_url: string;
  preview_width: number;
  preview_height: number;
  extent: { xmin: number; ymin: number; xmax: number; ymax: number };
  metadata: Record<string, unknown>;
  rgb_bands?: number[];
  http_port?: number;
}

export interface VectorPreview {
  __geo_type__: "vector_preview";
  geojson_url: string;
  feature_count: number;
  metadata: Record<string, unknown>;
  http_port?: number;
}

export type GeoPreview = RasterPreview | VectorPreview;

type BaseMap = "osm" | "satellite" | "none";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyLayer = any;

const BASE_LAYERS: Record<BaseMap, { label: string; create: () => AnyLayer | null }> = {
  osm: {
    label: "OSM Streets",
    create: () => new TileLayer({ source: new OSM(), visible: true }),
  },
  satellite: {
    label: "Satellite",
    create: () =>
      new TileLayer({
        source: new XYZ({
          url: "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",
          maxZoom: 19,
        }),
        visible: true,
      }),
  },
  none: {
    label: "None",
    create: () => null,
  },
};

export function parsePreview(output: string): GeoPreview | null {
  try {
    const obj = JSON.parse(output);
    if (obj.__geo_type__ === "raster_preview" || obj.__geo_type__ === "vector_preview") {
      return obj as GeoPreview;
    }
  } catch {
    // not valid JSON, try extracting JSON block
    const match = output.match(/\{[\s\S]*"__geo_type__"[\s\S]*\}/);
    if (match) {
      try {
        const obj = JSON.parse(match[0]);
        if (obj.__geo_type__ === "raster_preview" || obj.__geo_type__ === "vector_preview") {
          return obj as GeoPreview;
        }
      } catch {
        // ignore
      }
    }
  }
  return null;
}

function extentFromPreview(preview: GeoPreview): [number, number, number, number] {
  if (preview.__geo_type__ === "raster_preview") {
    const e = preview.extent;
    return [e.xmin, e.ymin, e.xmax, e.ymax];
  }
  // vector: will be auto-calculated from data
  return [0, 0, 0, 0];
}

export function GeoMapViewer({ output, className }: { output: string; className?: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<Map | null>(null);
  const [baseMap, setBaseMap] = useState<BaseMap>("osm");
  const [error, setError] = useState<string | null>(null);

  const preview = parsePreview(output);

  useEffect(() => {
    if (!containerRef.current || !preview) return;

    // Cleanup previous map
    if (mapRef.current) {
      mapRef.current.setTarget(undefined);
      mapRef.current = null;
    }

    try {
      const view = new View({
        center: fromLonLat([0, 0]),
        zoom: 2,
      });

      const layers: AnyLayer[] = [];

      // Base layer
      const baseCreator = BASE_LAYERS[baseMap];
      if (baseCreator) {
        const base = baseCreator.create();
        if (base) layers.push(base);
      }

      if (preview.__geo_type__ === "raster_preview" && preview.preview_url) {
        const extent = extentFromPreview(preview);
        const imgLayer = new ImageLayer({
          source: new ImageStatic({
            url: preview.preview_url,
            imageExtent: extent,
          }),
        });
        layers.push(imgLayer);

        // Fit to extent
        const w = extent[2] - extent[0];
        const h = extent[3] - extent[1];
        if (w > 0 && h > 0 && Number.isFinite(extent[0])) {
          view.fit(extent, { padding: [20, 20, 20, 20], maxZoom: 16 });
        }
      }

      const map = new Map({
        target: containerRef.current,
        layers,
        view,
        controls: [],
      });

      if (preview.__geo_type__ === "vector_preview" && preview.geojson_url) {
        // Load GeoJSON asynchronously
        fetch(preview.geojson_url)
          .then((r) => r.json())
          .then((geojson) => {
            const source = new VectorSource({
              features: new GeoJSON().readFeatures(geojson, {
                featureProjection: "EPSG:3857",
              }),
            });
            const vecLayer = new VectorLayer({ source });
            map.addLayer(vecLayer);
            if (source.getExtent() && !isNaN(source.getExtent()![0])) {
              map.getView().fit(source.getExtent()!, {
                padding: [20, 20, 20, 20],
                maxZoom: 16,
              });
            }
          })
          .catch((err) => {
            setError(`Failed to load GeoJSON: ${err.message}`);
          });
      }

      mapRef.current = map;
    } catch (err: unknown) {
      setError(`Map init failed: ${err instanceof Error ? err.message : String(err)}`);
    }

    return () => {
      if (mapRef.current) {
        mapRef.current.setTarget(undefined);
      }
    };
  }, [output, baseMap]);

  if (error) {
    return <div className="geomap__error">{error}</div>;
  }

  if (!preview) return null;

  return (
    <div className={`geomap ${className ?? ""}`}>
      <div className="geomap__toolbar">
        <span className="geomap__title">
          {preview.__geo_type__ === "raster_preview" ? "Raster Preview" : "Vector Preview"}
        </span>
        <select
          className="geomap__basemap"
          value={baseMap}
          onChange={(e) => setBaseMap(e.target.value as BaseMap)}
        >
          {Object.entries(BASE_LAYERS).map(([key, { label }]) => (
            <option key={key} value={key}>
              {label}
            </option>
          ))}
        </select>
      </div>
      <div ref={containerRef} className="geomap__canvas" />
      {preview.__geo_type__ === "raster_preview" && (
        <div className="geomap__info">
          {preview.preview_width}x{preview.preview_height} ·{" "}
          {preview.rgb_bands ? `RGB(${preview.rgb_bands.join(",")})` : ""}
        </div>
      )}
    </div>
  );
}
