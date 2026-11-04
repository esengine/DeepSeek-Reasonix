import { t } from "../i18n";

/** One line of a unified diff with the line it stands on before and after; a
 *  hunk header carries neither. */
export interface DiffLine { old?: number; now?: number; mark: " " | "+" | "-" | "@"; text: string }

export function diffLines(diff: string): DiffLine[] {
  const out: DiffLine[] = [];
  let old = 0;
  let now = 0;
  const lines = diff.replace(/\n$/, "").split("\n");
  for (const line of lines) {
    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
    if (hunk) {
      old = Number(hunk[1]);
      now = Number(hunk[2]);
      out.push({ mark: "@", text: line });
    } else if (!out.length || line.startsWith("\\")) {
      // File headers come before the first hunk; inside one, a removed line may
      // itself begin with "--", so only git's "\ No newline" note is skipped.
      continue;
    } else if (line.startsWith("+")) {
      out.push({ now: now++, mark: "+", text: line.slice(1) });
    } else if (line.startsWith("-")) {
      out.push({ old: old++, mark: "-", text: line.slice(1) });
    } else {
      out.push({ old: old++, now: now++, mark: " ", text: line.slice(1) });
    }
  }
  return out;
}

/** A diff as the workbench reads one: each line numbered before and after, the
 *  @@ header kept, a click on a number asking for the file itself. */
export function UnifiedDiff({ diff, label, truncated = false, onLine, target }: { diff: string; label: string; truncated?: boolean; onLine?: () => void; target?: string }) {
  const lines = diffLines(diff);
  if (lines.length === 0) return <p className="wb-change-wait">{t("workbench.no_changes")}</p>;
  return (
    <div className="wb-diff" role="table" aria-label={label}>
      {lines.map((l, i) => (
        <div key={i} className="wb-diff-line" data-mark={l.mark} role="row">
          <button type="button" className="wb-diff-n" data-action="workbench.change-open" data-target={target} tabIndex={-1} aria-hidden="true" onClick={onLine}>{l.old ?? ""}</button>
          <button type="button" className="wb-diff-n" data-action="workbench.change-open" data-target={target} tabIndex={-1} aria-hidden="true" onClick={onLine}>{l.now ?? ""}</button>
          <span className="wb-diff-mark">{l.mark === "@" ? "" : l.mark.trim()}</span>
          <span className="wb-diff-text">{l.text}</span>
        </div>
      ))}
      {truncated && <p className="wb-change-wait">{t("workbench.diff_cut_short")}</p>}
    </div>
  );
}
