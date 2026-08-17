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
  return null;
}
