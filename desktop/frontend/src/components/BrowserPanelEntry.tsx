import "./BrowserPanel.css";

import { BrowserDockTab, BrowserPanel } from "./BrowserPanel";

export type BrowserSurfaceProps =
  | { surface: "tab"; active: boolean; onSelect: () => void }
  | { surface: "panel"; taskId: string | undefined };

// One lazy boundary for the whole dock surface: the tab button and the panel
// share this chunk, so the initial bundle carries no browser code at all.
export default function BrowserSurface(props: BrowserSurfaceProps) {
  return props.surface === "tab" ? <BrowserDockTab active={props.active} onSelect={props.onSelect} /> : <BrowserPanel taskId={props.taskId} />;
}
