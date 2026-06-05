import { lazy, Suspense } from "react";
import { CopyButton } from "./CopyButton";

export interface EditorProps {
  value: string;
  language?: string;
  readOnly?: boolean;
  maxHeight?: number;
}

// ── EDITOR SEAM (code) ───────────────────────────────────────────────────────
// Every code view in the app renders through this component, so upgrading the
// editor is a one-line change here — swap the lazily-imported module:
//
//   ./editors/HljsCode         current — highlight.js read-only view
//   ./editors/MonacoCode       pnpm add @monaco-editor/react monaco-editor
//   ./editors/CodeMirrorCode   pnpm add @uiw/react-codemirror @codemirror/lang-*
//
// The replacement only has to honor EditorProps. It's lazy-loaded so a heavy
// editor (~MBs) never lands in the initial bundle — it streams in the first time
// a code block or tool result is shown. See desktop/README.md ("Editor seam").
const Impl = lazy(() => import("./editors/HljsCode"));

export function CodeViewer(props: EditorProps) {
  return (
    <div className="code-block">
      <CopyButton text={props.value} className="code-block__copy" />
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
