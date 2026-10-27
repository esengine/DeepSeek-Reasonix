import { isolateHistory, redo, redoDepth, selectAll, undo, undoDepth } from "@codemirror/commands";
import { EditorView } from "@codemirror/view";
import { t } from "../i18n";
import { copyText } from "./CopyButton";
import type { TextMenuSession } from "./workbench_text_menu";

export function editorTextSession(editor: EditorView, live: () => boolean): TextMenuSession {
  const { doc, selection } = editor.state;
  const editable = !editor.state.readOnly && editor.state.facet(EditorView.editable);
  const text = selection.ranges.map(({ from, to }) => doc.sliceString(from, to)).join(editor.state.lineBreak);
  const valid = () => live() && editor.state.doc === doc && editable === (!editor.state.readOnly && editor.state.facet(EditorView.editable));
  const focus = (restore = true) => {
    if (!live()) return;
    if (restore && valid()) editor.dispatch({ selection });
    editor.focus();
  };
  return {
    anchor: editor.contentDOM, text, editable, selected: selection.ranges.some((range) => !range.empty),
    canUndo: editable && undoDepth(editor.state) > 0, canRedo: editable && redoDepth(editor.state) > 0,
    valid, focus,
    async run(action, current) {
      if (!current()) return;
      if (action === "copy" || action === "cut") {
        if (!selection.ranges.some((range) => !range.empty)) return;
        await copyText(text);
        if (!current()) return;
        focus();
        if (action === "cut" && editable) editor.dispatch({
          ...editor.state.replaceSelection(""), userEvent: "delete.cut", annotations: isolateHistory.of("full"),
        });
        return;
      }
      if (action === "paste") {
        if (!editable) return;
        if (!navigator.clipboard?.readText) throw new Error(t("此浏览器不允许读取剪贴板，请使用键盘粘贴"));
        const text = await navigator.clipboard.readText();
        if (!current()) return;
        focus();
        editor.dispatch({ ...editor.state.replaceSelection(text), userEvent: "input.paste", annotations: isolateHistory.of("full"), scrollIntoView: true });
        return;
      }
      focus();
      if (action === "selectAll") selectAll(editor);
      else if (editable && action === "undo") undo(editor);
      else if (editable && action === "redo") redo(editor);
    },
  };
}
