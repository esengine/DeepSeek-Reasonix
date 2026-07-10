import { useCallback, useRef } from "react";
import Editor, { type Monaco, type OnMount } from "@monaco-editor/react";
import type { editor as MonacoEditor } from "monaco-editor";
import { CopyButton } from "./CopyButton";
import { normalizeMonacoSelectionRange, type WorkspaceLineRange } from "../lib/workspaceLineReference";

interface WorkspaceMonacoPreviewProps {
  value: string;
  language?: string;
  path: string;
  onSelectionContextMenu?: (event: { path: string } & WorkspaceLineRange) => void;
}

const MONACO_THEME = "reasonix-workspace-preview";

function cssColor(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  return styles.getPropertyValue(name).trim() || fallback;
}

function defineReasonixMonacoTheme(monaco: Monaco, node: HTMLElement | null) {
  const root = node ?? document.documentElement;
  const styles = getComputedStyle(root);
  const docStyles = getComputedStyle(document.documentElement);
  const bg = cssColor(styles, "--workspace-preview-bg", cssColor(docStyles, "--bg", "#0b0e12"));
  const fg = cssColor(docStyles, "--fg", "#d8dee9");
  const muted = cssColor(docStyles, "--fg-muted", "#8792a2");
  const border = cssColor(docStyles, "--border-soft", "#242832");
  const selection = cssColor(docStyles, "--selection-bg", "#264f78");
  const isLight = document.documentElement.dataset.theme === "light" || window.matchMedia?.("(prefers-color-scheme: light)").matches;
  monaco.editor.defineTheme(MONACO_THEME, {
    base: isLight ? "vs" : "vs-dark",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": bg,
      "editor.foreground": fg,
      "editorLineNumber.foreground": muted,
      "editorLineNumber.activeForeground": fg,
      "editor.selectionBackground": selection,
      "editor.lineHighlightBackground": isLight ? "#00000008" : "#ffffff08",
      "editor.lineHighlightBorder": "#00000000",
      "editorWidget.background": bg,
      "editorWidget.border": border,
    },
  });
}

export function WorkspaceMonacoPreview({
  value,
  language,
  path,
  onSelectionContextMenu,
}: WorkspaceMonacoPreviewProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<MonacoEditor.IStandaloneCodeEditor | null>(null);

  const handleMount = useCallback<OnMount>(
    (editor, monaco) => {
      editorRef.current = editor;
      defineReasonixMonacoTheme(monaco, rootRef.current);
      monaco.editor.setTheme(MONACO_THEME);
      editor.onContextMenu((event) => {
        const range = normalizeMonacoSelectionRange(editor.getSelection());
        if (!range) return;
        event.event.preventDefault();
        event.event.stopPropagation();
        onSelectionContextMenu?.({
          path,
          ...range,
        });
      });
    },
    [onSelectionContextMenu, path],
  );

  return (
    <div className="workspace-monaco-preview" ref={rootRef}>
      <Editor
        value={value}
        language={language}
        theme={MONACO_THEME}
        onMount={handleMount}
        options={{
          readOnly: true,
          domReadOnly: true,
          automaticLayout: true,
          minimap: { enabled: false },
          lineNumbers: "on",
          glyphMargin: true,
          folding: true,
          foldingHighlight: true,
          foldingStrategy: "auto",
          showFoldingControls: "mouseover",
          contextmenu: false,
          scrollBeyondLastLine: false,
          guides: { indentation: true, bracketPairs: true },
          bracketPairColorization: { enabled: true },
          matchBrackets: "always",
          renderLineHighlight: "line",
          wordWrap: "off",
          fontLigatures: false,
          tabSize: 2,
        }}
      />
      <CopyButton text={value} className="code-block__copy" />
    </div>
  );
}

export default WorkspaceMonacoPreview;
