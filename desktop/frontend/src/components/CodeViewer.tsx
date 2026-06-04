import { lazy, Suspense, useState } from "react";
import { CodeBlockToolbar } from "./CodeBlockToolbar";

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
  // The toolbar lives above the editor and stays in sync with the language
  // override (the user's manual pick in the picker). We hold the override
  // here rather than inside the toolbar so a future feature — e.g. "use
  // this highlight for the next N code blocks" — can read it from the
  // parent. The override is "additive" to props.language: when the user
  // picks "rust", we pass that down; if they then change the input (which
  // would mean a different code block), the toolbar's internal override
  // resets to the new props.language.
  const [override, setOverride] = useState<string | null>(null);
  const effective = override ?? props.language;
  return (
    <div className="code-block">
      <CodeBlockToolbar
        value={props.value}
        language={effective}
        onLanguageChange={(lang) => setOverride(lang)}
      />
      <Suspense
        fallback={
          <pre className="code code--loading">
            <code>{props.value}</code>
          </pre>
        }
      >
        <Impl {...props} language={effective} />
      </Suspense>
    </div>
  );
}
