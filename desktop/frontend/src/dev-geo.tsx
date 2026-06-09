/**
 * Dev entry point — renders GeoMapViewer with MCP server output.
 * Open dev-geo.html in browser when MCP server (geo_env) is running.
 */
import { createRoot } from "react-dom/client";
import { GeoMapViewer } from "./components/geo/GeoMapViewer";
import { GeoTable } from "./components/geo/GeoTable";
import "ol/ol.css";

// The MCP server writess geo_output.json to TEMP after each probe.
// For dev, we hardcode the output captured from the last read_geo_data call.
const MOCK_RASTER_OUTPUT = {
  __geo_type__: "raster_preview",
  preview_url: "http://127.0.0.1:60068/preview/5cd6bdeec98540e4ae00af8c0bddcbd1.webp",
  preview_width: 256,
  preview_height: 256,
  preview_size_bytes: 27850,
  rgb_bands: [1, 1, 1],
  http_port: 60068,
  extent: { xmin: 116.0, xmax: 116.256, ymin: 39.744, ymax: 40.0 },
  metadata: {
    data_type: "raster",
    driver: "GTiff",
    crs: "EPSG:4326",
    size: { width: 256, height: 256 },
    band_count: 1,
    bands: [{
      index: 1, data_type: "Float32", nodata: -999,
      color_interp: "Gray",
      statistics: { min: 0.31, max: 0.94, mean: 0.60, stddev: 0.09 }
    }]
  }
};

function App() {
  const rasterJson = JSON.stringify(MOCK_RASTER_OUTPUT);

  return (
    <div>
      <h2>Raster Preview — GeoMapViewer</h2>
      <GeoMapViewer output={rasterJson} />

      <h2>Raw Tool Output (read_geo_data)</h2>
      <pre style={{
        background: "#1a1a2e", padding: 12, borderRadius: 6,
        fontSize: 11, overflow: "auto", maxHeight: 300
      }}>
        {JSON.stringify(MOCK_RASTER_OUTPUT, null, 2)}
      </pre>
    </div>
  );
}

const root = document.getElementById("root");
if (root) createRoot(root).render(<App />);
