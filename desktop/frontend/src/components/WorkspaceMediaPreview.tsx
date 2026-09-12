import type { FilePreview } from "../lib/types";
import { workspaceBasename } from "../lib/workspacePanelFormat";

export function WorkspaceMediaPreview({ preview }: { preview: FilePreview }) {
  if (!preview.url) return null;
  if (preview.kind === "image") {
    return (
      <div className="workspace-media workspace-media--image">
        <img src={preview.url} alt={workspaceBasename(preview.path)} decoding="async" draggable={false} />
      </div>
    );
  }
  if (preview.kind === "pdf") {
    return <iframe className="workspace-media workspace-media--pdf" src={preview.url} title={workspaceBasename(preview.path)} />;
  }
  if (preview.kind === "html") {
    return <iframe className="workspace-media workspace-media--html" src={preview.url} title={workspaceBasename(preview.path)} sandbox="allow-scripts" />;
  }
  if (preview.kind === "audio") {
    return <div className="workspace-media workspace-media--audio"><audio src={preview.url} controls preload="metadata" /></div>;
  }
  if (preview.kind === "video") {
    return <div className="workspace-media workspace-media--video"><video src={preview.url} controls preload="metadata" /></div>;
  }
  return null;
}
