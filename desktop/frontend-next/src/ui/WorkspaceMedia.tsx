import type { AgentPort } from "../port/port";
import { WorkspaceImage, isWorkspaceImage } from "./WorkspaceImage";
import { WorkspaceVideo, isWorkspaceVideo } from "./WorkspaceVideo";

export { ProductionManifest, isProductionManifest } from "./ProductionManifest";

export const isWorkspaceMedia = (path: string) => isWorkspaceImage(path) || isWorkspaceVideo(path);

export function WorkspaceMedia({ port, path }: { port: AgentPort; path: string }) {
  return isWorkspaceVideo(path) ? <WorkspaceVideo port={port} path={path} /> : <WorkspaceImage port={port} path={path} />;
}
