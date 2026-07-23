import { lazy, Suspense } from "react";

export interface EditorProps {
  value: string;
  language?: string;
  readOnly?: boolean;
  maxHeight?: number;
  /** Original source size in bytes when the caller already has it. */
  sourceSize?: number;
  /** Opt in to the workspace-oriented viewer with line numbers and search. */
  showLineNumbers?: boolean;
}

// ── EDITOR SEAM (code) ───────────────────────────────────────────────────────
// Keep the established highlighted viewer for existing chat, diff, and tool
// surfaces. Workspace previews explicitly opt into the heavier searchable
// viewer, so this feature cannot silently change every code block in the app.
const HljsImpl = lazy(() => import("./editors/HljsCode"));
const LineNumberImpl = lazy(() => import("./editors/LineNumberCode"));

export function CodeViewer(props: EditorProps) {
  const Impl = props.showLineNumbers ? LineNumberImpl : HljsImpl;
  return (
    <div className="code-block">
      <Suspense
        fallback={
          <pre className="code code--loading">
            <code>{props.value}</code>
          </pre>
        }
      >
        <Impl {...props} />
      </Suspense>
    </div>
  );
}
