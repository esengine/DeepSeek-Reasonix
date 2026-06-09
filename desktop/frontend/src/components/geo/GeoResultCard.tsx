import { CodeViewer } from "../CodeViewer";
import { GeoMapViewer, parsePreview } from "./GeoMapViewer";
import { GeoTable } from "./GeoTable";

export { parsePreview };

/** GeoResultCard renders the body of a geo tool result — map for raster, map+table for vector. */
export function GeoResultCard({ output }: { output: string }) {
  const preview = parsePreview(output);
  if (!preview) {
    return <CodeViewer value={output} maxHeight={280} />;
  }

  return (
    <div className="geo-result">
      <GeoMapViewer output={output} />
      {preview.__geo_type__ === "vector_preview" && preview.geojson_url && (
        <GeoTable
          geojsonUrl={preview.geojson_url}
          featureCount={preview.feature_count}
        />
      )}
      <details className="geo-result__raw">
        <summary>Raw metadata (JSON)</summary>
        <CodeViewer value={JSON.stringify(preview, null, 2)} language="json" maxHeight={200} />
      </details>
    </div>
  );
}
