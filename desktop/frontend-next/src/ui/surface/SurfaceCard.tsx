import type { SurfaceActionPort } from "../../port/surface";
import { lazySurface } from "./a2ui/lazy";
import type { Item } from "../../state/session";
import { parseSurface } from "./spec";
import { ExtensionView } from "../cards/ExtensionView";
import { t } from "../../i18n";
import "../RestoreNotice.css";
import type { Tool } from "../../port/wire";
import { surfaceOfTool, surfaceFields, surfaceActions, actionSurfaceOfTool } from "./call";
import "./SurfaceCard.css";
import { SurfaceMediaContext, type SurfaceMediaPort } from "./media-context";

const NativeSurface = lazySurface(() => import("./a2ui/form"));

const NativeAction = lazySurface(()=>import("./a2ui/action"));

export function surfaceTool(tool: Tool, running: boolean, port?: Partial<SurfaceActionPort & SurfaceMediaPort>) {
  const spec = surfaceOfTool(tool);
  if (spec && surfaceActions(spec).length) {
    if (running) return null;
    if (!actionSurfaceOfTool(tool)) return undefined;
    return <SurfaceMediaContext.Provider value={port}><NativeAction key={tool.id} spec={spec} callId={tool.id ?? ""} port={port?.surfaceAction && port.surfaceActionState && port.cancelQueued ? port as SurfaceActionPort : undefined}/></SurfaceMediaContext.Provider>;
  }
  if (spec && surfaceFields(spec).length) return null;
  if (spec && !running && tool.output !== undefined) return <ExtensionView body={spec.body} />;
  return undefined;
}

export interface SurfaceCardProps {
  port?: SurfaceMediaPort;
  item: Extract<Item, { t: "ask" }>;
  onAnswer: (itemId: string, id: string, answers: { questionId: string; selected: string[]; value?: Record<string, unknown> }[]) => Promise<void>;
}

export function SurfaceCard(props: SurfaceCardProps) {
  const spec = parseSurface(props.item.ask.questions[0]?.surface);
  if (!spec || props.item.surfaceExpired) return <div className="restore-notice" role="status" data-code="surface.expired">{t("chat.this_surface_has_expired_wait")}</div>;
  return <SurfaceMediaContext.Provider value={props.port}><NativeSurface key={props.item.ask.id} {...props} spec={spec} /></SurfaceMediaContext.Provider>;
}
