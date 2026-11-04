import { useEffect, useRef, useState } from "react";
import Map, { Layer, Marker, NavigationControl, Source, type MapRef } from "react-map-gl/maplibre";
import { Alert, Button } from "@mantine/core";
import { setWorkerUrl, type StyleSpecification } from "maplibre-gl";
import workerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";
import type { SurfaceOption } from "../../../port/wire";
import { t } from "../../../i18n";
import "maplibre-gl/dist/maplibre-gl.css";

setWorkerUrl(workerUrl);

const style: StyleSpecification = { version: 8, sources: { streets: { type: "raster", tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"], tileSize: 256, maxzoom: 19, attribution: '© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener noreferrer">OpenStreetMap</a> contributors' } }, layers: [{ id: "streets", type: "raster", source: "streets" }] };

export default function MapCanvas({ places, order, active, focusVersion, focus }: { places: SurfaceOption[]; order: string[]; active?: string; focusVersion: number; focus: (id: string) => void }) {
  const ref = useRef<MapRef>(null);
  const [error, setError] = useState(false);
  const [ready, setReady] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const coordinates = places.map(p => [p.location!.longitude, p.location!.latitude] as [number, number]);
  const selected = order.flatMap(id => { const p = places.find(o => o.value === id); return p ? [[p.location!.longitude, p.location!.latitude]] : []; });
  const bounds = JSON.stringify(coordinates);
  const overview = () => {
    if (!ready || !ref.current || !coordinates.length) return;
    const xs = coordinates.map(p => p[0]), ys = coordinates.map(p => p[1]);
    ref.current.fitBounds([[Math.min(...xs), Math.min(...ys)], [Math.max(...xs), Math.max(...ys)]], { padding: 55, maxZoom: 14, duration: 0 });
  };
  useEffect(overview, [bounds, ready]);
  const point = places.find(p => p.value === active)?.location;
  useEffect(() => { if (point && ready && focusVersion > 0) ref.current?.jumpTo({ center: [point.longitude, point.latitude] }); }, [point?.longitude, point?.latitude, ready, focusVersion]);
  return <div className="rich-map" aria-label={t("chat.places_map")}>
    <Map key={attempt} ref={ref} initialViewState={{ longitude: coordinates[0]?.[0] ?? 0, latitude: coordinates[0]?.[1] ?? 0, zoom: 11 }} mapStyle={style} onLoad={() => { setReady(true); setError(false); }} onError={() => setError(true)} attributionControl={{ compact: false }}>
      <NavigationControl position="top-right" showCompass={false} />
      {selected.length > 1 && <Source id="visit-order" type="geojson" data={{ type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: selected } }}><Layer id="visit-order-line" type="line" paint={{ "line-color": "#0d9488", "line-width": 3, "line-dasharray": [2, 2] }} /></Source>}
      {places.map(p => <Marker key={p.value} longitude={p.location!.longitude} latitude={p.location!.latitude} anchor="center"><Button className="rich-map-pin" radius="xl" color={active === p.value ? "dark" : "teal"} size="compact-sm" aria-label={p.label} data-action="surface.explore" onClick={() => focus(p.value)}>{order.includes(p.value) ? order.indexOf(p.value) + 1 : "+"}</Button></Marker>)}
    </Map>
    <Button className="rich-map-overview" size="xs" variant="default" disabled={!ready || !coordinates.length} data-action="surface.explore" onClick={overview}>{t("chat.show_all_places")}</Button>
    {error && <Alert className="rich-map-status" color="yellow" role="status">{t("chat.some_map_content_could_not")} <Button size="xs" variant="subtle" data-action="surface.explore" onClick={() => { setReady(false); setError(false); setAttempt(v => v + 1); }}>{t("common.retry")}</Button></Alert>}
  </div>;
}
