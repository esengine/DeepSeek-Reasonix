import { useState } from "react";
import { t } from "../i18n";
import type { PickedElement } from "../port/host";
import type { SourceEdit, StyleProperty } from "./htmlsource";
import type { PageEditFailure, PageEditor } from "./pageedit";

const FAILED: Record<PageEditFailure, () => string> = {
  not_found: () => t("workbench.edit_not_found"),
  ambiguous: () => t("workbench.edit_ambiguous"),
  unmappable: () => t("workbench.edit_unmappable"),
  mismatch: () => t("workbench.edit_mismatch"),
  not_plain_text: () => t("workbench.edit_not_plain_text"),
  bad_value: () => t("workbench.edit_bad_value"),
  outside: () => t("workbench.edit_outside"),
  conflict: () => t("workbench.edit_conflict"),
  failed: () => t("workbench.edit_failed"),
};

const STYLES: { property: StyleProperty; label: () => string }[] = [
  { property: "color", label: () => t("workbench.edit_color") },
  { property: "backgroundColor", label: () => t("workbench.edit_background") },
  { property: "fontSize", label: () => t("workbench.edit_font_size") },
];

interface Props {
  element: PickedElement;
  /** The disk path of the file the page was opened from, or "" for any other page. */
  filePath: string;
  editor?: PageEditor;
  onSaved: () => void;
  onClose: () => void;
}

export function BrowserElementEdit({ element: picked, filePath, editor, onSaved, onClose }: Props) {
  // What the file holds now: a save moves it, so the next one is measured from there.
  const [element, setElement] = useState(picked);
  const [text, setText] = useState(picked.text ?? "");
  const [styles, setStyles] = useState<Partial<Record<StyleProperty, string>>>(picked.style ?? {});
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ kind: "ok" | "error"; text: string } | null>(null);
  const editable = !!filePath && !!editor;

  const changes = (): SourceEdit[] => {
    const out: SourceEdit[] = [];
    if (element.text && text !== element.text) out.push({ kind: "text", before: element.text, after: text });
    for (const { property } of STYLES) {
      const value = (styles[property] ?? "").trim();
      if (value && value !== element.style?.[property]) out.push({ kind: "style", property, value });
    }
    return out;
  };

  const save = async () => {
    const edits = changes();
    if (!editor || !edits.length) {
      setNote({ kind: "error", text: t("workbench.edit_nothing") });
      return;
    }
    setBusy(true);
    setNote(null);
    const out = await editor(filePath, { selector: element.selector, tagName: element.tagName }, edits);
    setBusy(false);
    if (out.saved) {
      setElement({ ...element, text: element.text ? text : element.text, style: { ...element.style, ...Object.fromEntries(edits.flatMap((e) => (e.kind === "style" ? [[e.property, e.value]] : []))) } });
      setNote({ kind: "ok", text: t("workbench.edit_saved", { path: out.path }) });
      onSaved();
    } else {
      setNote({ kind: "error", text: FAILED[out.code]() });
    }
  };

  return (
    <form className="belement" aria-label={t("workbench.edit_picked_element")} data-action-submit="browser.edit-save" onSubmit={(event) => { event.preventDefault(); void save(); }}>
      <header className="belement-head">
        <code title={element.selector}>{element.selector}</code>
        <button type="button" data-action="browser.edit-close" onClick={onClose}>{t("workbench.edit_close")}</button>
      </header>
      {!editable && <p className="studio-browser-note">{t("workbench.edit_not_file")}</p>}
      {element.text !== undefined && element.text !== "" && (
        <label className="belement-field">
          <span>{t("workbench.edit_text")}</span>
          <input value={text} disabled={!editable || busy} onChange={(event) => setText(event.target.value)} spellCheck={false} />
        </label>
      )}
      {STYLES.map(({ property, label }) => (
        <label className="belement-field" key={property}>
          <span>{label()}</span>
          <input value={styles[property] ?? ""} disabled={!editable || busy} onChange={(event) => setStyles((v) => ({ ...v, [property]: event.target.value }))} spellCheck={false} />
        </label>
      ))}
      <div className="belement-foot">
        <button type="submit" data-action="browser.edit-save" disabled={!editable || busy}>{busy ? t("workbench.edit_saving") : t("workbench.edit_save")}</button>
        {note && <p role={note.kind === "error" ? "alert" : "status"} className={note.kind === "error" ? "studio-browser-error" : "studio-browser-note"}>{note.text}</p>}
      </div>
    </form>
  );
}
