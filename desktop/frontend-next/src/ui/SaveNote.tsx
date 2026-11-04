import { t } from "../i18n";

export interface SaveNoteState {
  text: string;
  unapplied: boolean;
}

/** The outcome of a refused or half-applied settings save: warn when it was written but not yet in effect, error otherwise. */
export function SaveNote({ note }: { note: SaveNoteState | null }) {
  if (!note) return null;
  return (
    <div className="find" data-lvl={note.unapplied ? "warn" : "err"} role={note.unapplied ? "status" : "alert"}>
      <span className="t">{t(note.unapplied ? "common.saved_not_applied_yet" : "common.that_did_not_go_through")}</span>
      <span className="why">{note.text}</span>
    </div>
  );
}
