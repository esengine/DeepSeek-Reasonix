import { useEffect, useRef } from "react";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { gotoLine, highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import {
  bracketMatching,
  foldGutter,
  foldKeymap,
  HighlightStyle,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { t } from "../i18n";

// Loaded per file, so opening a Go file does not pay for every other grammar.
const LANGUAGE: Record<string, () => Promise<Extension>> = {
  ts: () => import("@codemirror/lang-javascript").then((m) => m.javascript({ typescript: true })),
  tsx: () => import("@codemirror/lang-javascript").then((m) => m.javascript({ typescript: true, jsx: true })),
  js: () => import("@codemirror/lang-javascript").then((m) => m.javascript()),
  mjs: () => import("@codemirror/lang-javascript").then((m) => m.javascript()),
  cjs: () => import("@codemirror/lang-javascript").then((m) => m.javascript()),
  jsx: () => import("@codemirror/lang-javascript").then((m) => m.javascript({ jsx: true })),
  go: () => import("@codemirror/lang-go").then((m) => m.go()),
  py: () => import("@codemirror/lang-python").then((m) => m.python()),
  rs: () => import("@codemirror/lang-rust").then((m) => m.rust()),
  java: () => import("@codemirror/lang-java").then((m) => m.java()),
  json: () => import("@codemirror/lang-json").then((m) => m.json()),
  css: () => import("@codemirror/lang-css").then((m) => m.css()),
  html: () => import("@codemirror/lang-html").then((m) => m.html()),
  htm: () => import("@codemirror/lang-html").then((m) => m.html()),
  md: () => import("@codemirror/lang-markdown").then((m) => m.markdown()),
  sql: () => import("@codemirror/lang-sql").then((m) => m.sql()),
  xml: () => import("@codemirror/lang-xml").then((m) => m.xml()),
  svg: () => import("@codemirror/lang-xml").then((m) => m.xml()),
  yaml: () => import("@codemirror/lang-yaml").then((m) => m.yaml()),
  yml: () => import("@codemirror/lang-yaml").then((m) => m.yaml()),
};

const extensionOf = (path: string) => path.slice(path.lastIndexOf(".") + 1).toLowerCase();

// The palette is the theme's own variables, so a pack or a scheme change repaints
// the editor with everything else instead of through a second colour table.
const highlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.tagName, tags.modifier, tags.operatorKeyword], color: "var(--syn-key)" },
  { tag: [tags.string, tags.regexp, tags.special(tags.string), tags.attributeValue], color: "var(--syn-str)" },
  { tag: [tags.number, tags.bool, tags.null, tags.atom], color: "var(--syn-num)" },
  {
    tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.typeName, tags.className],
    color: "var(--syn-fn)",
  },
  { tag: [tags.comment, tags.meta], color: "var(--faint)", fontStyle: "italic" },
  { tag: tags.heading, fontWeight: "bold" },
  { tag: tags.link, textDecoration: "underline" },
]);

const theme = (dark: boolean) =>
  EditorView.theme(
    {
      "&": { height: "100%", color: "var(--text)", backgroundColor: "var(--face-code)", fontSize: "12px" },
      "&.cm-focused": { outline: "none" },
      ".cm-scroller": { fontFamily: "var(--mono)", lineHeight: "1.65" },
      ".cm-content": { caretColor: "var(--text)", paddingBottom: "80px" },
      ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--text)" },
      ".cm-gutters": { backgroundColor: "var(--face-code)", color: "var(--ghost)", border: "none" },
      ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--muted)" },
      ".cm-activeLine": { backgroundColor: "color-mix(in srgb, var(--overlay) 55%, transparent)" },
      "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
        backgroundColor: "color-mix(in srgb, var(--accent) 28%, transparent)",
      },
      ".cm-searchMatch": { backgroundColor: "color-mix(in srgb, var(--warn) 30%, transparent)" },
      ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "color-mix(in srgb, var(--accent) 45%, transparent)" },
      ".cm-selectionMatch": { backgroundColor: "color-mix(in srgb, var(--accent) 14%, transparent)" },
      ".cm-panels": { backgroundColor: "var(--raised)", color: "var(--text)" },
      ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--border)" },
      ".cm-panel input, .cm-panel button, .cm-textfield": {
        fontFamily: "var(--ui)", fontSize: "11px", color: "var(--text)",
      },
      ".cm-textfield": {
        backgroundColor: "var(--surface)", border: "1px solid var(--border)", borderRadius: "5px", padding: "2px 6px",
      },
      ".cm-button": {
        backgroundImage: "none", backgroundColor: "var(--overlay)", border: "1px solid var(--border)", borderRadius: "5px",
      },
      ".cm-foldPlaceholder": { backgroundColor: "var(--overlay)", border: "none", color: "var(--muted)" },
    },
    { dark },
  );

// The search and go-to-line panels speak the window's language.
const phrases = () =>
  EditorState.phrases.of({
    Find: t("查找"),
    Replace: t("替换"),
    next: t("下一个"),
    previous: t("上一个"),
    all: t("全部"),
    "match case": t("区分大小写"),
    regexp: t("正则"),
    "by word": t("全词匹配"),
    replace: t("替换"),
    "replace all": t("全部替换"),
    close: t("关闭"),
    "Go to line": t("跳到行"),
    go: t("跳转"),
  });

interface Props {
  path: string;
  value: string;
  dark: boolean;
  onChange: (text: string) => void;
  onSave: () => void;
}

/** A file, read and edited in one place: highlighted as it is typed, with the
 *  editor's own find (Ctrl+F), go to line (Ctrl+G) and save (Ctrl+S). */
export default function CodeEditor({ path, value, dark, onChange, onSave }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const handlers = useRef({ onChange, onSave });
  handlers.current = { onChange, onSave };
  const language = useRef(new Compartment());
  const look = useRef(new Compartment());

  useEffect(() => {
    if (!host.current) return;
    const editor = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          foldGutter(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          drawSelection(),
          history(),
          indentOnInput(),
          bracketMatching(),
          highlightSelectionMatches(),
          search({ top: true }),
          syntaxHighlighting(highlight),
          phrases(),
          keymap.of([
            { key: "Mod-s", preventDefault: true, run: () => (handlers.current.onSave(), true) },
            { key: "Mod-g", preventDefault: true, run: gotoLine },
            indentWithTab,
            ...searchKeymap,
            ...foldKeymap,
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) handlers.current.onChange(update.state.doc.toString());
          }),
          language.current.of([]),
          look.current.of(theme(dark)),
        ],
      }),
    });
    view.current = editor;
    return () => {
      editor.destroy();
      view.current = null;
    };
    // One editor per file: the parent keys this component by path.
  }, []);

  useEffect(() => {
    let live = true;
    const load = LANGUAGE[extensionOf(path)];
    void load?.().then((ext) => {
      if (live) view.current?.dispatch({ effects: language.current.reconfigure(ext) });
    });
    return () => {
      live = false;
    };
  }, [path]);

  useEffect(() => {
    view.current?.dispatch({ effects: look.current.reconfigure(theme(dark)) });
  }, [dark]);

  // A save or a reload hands back text the editor may not hold; the reader's own
  // typing arrives here as the same text and changes nothing.
  useEffect(() => {
    const editor = view.current;
    if (!editor || editor.state.doc.toString() === value) return;
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } });
  }, [value]);

  return <div className="workbench-editor" ref={host} aria-label={t("文件内容")} />;
}
