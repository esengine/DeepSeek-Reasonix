import { plural, t } from "../i18n";
import { tokens } from "../i18n/format";
import type { WorkspaceChange } from "../port/workspace";

export interface ChangeTotals {
  files: number;
  insertions: number;
  deletions: number;
  counted: boolean;
}

export function totalsOf(changes: readonly WorkspaceChange[]): ChangeTotals {
  let insertions = 0;
  let deletions = 0;
  let counted = false;
  for (const c of changes) {
    if (c.insertions !== undefined) {
      insertions += c.insertions;
      counted = true;
    }
    if (c.deletions !== undefined) {
      deletions += c.deletions;
      counted = true;
    }
  }
  return { files: changes.length, insertions, deletions, counted };
}

// The workspace's differing files as git counts them, in one place above the
// input box. It reads the same list as the workbench's change section and
// opens it; nothing here is the turn's own edits.
export function ChangedFilesPill({ changes, onOpen }: { changes: readonly WorkspaceChange[]; onOpen: () => void }) {
  if (changes.length === 0) return null;
  const sum = totalsOf(changes);
  const label = plural(sum.files, "composer.n_1_file_changed", "composer.n_files_changed", { n: tokens(sum.files) });
  const aria = sum.counted
    ? t("composer.label_up_lines_added_down", { label, up: sum.insertions, down: sum.deletions })
    : t("composer.label_open_file_changes", { label });
  return (
    <div className="changedpill-row">
      <button className="changedpill" data-action="changes.open" aria-label={aria} title={t("composer.view_file_changes")} onClick={onOpen}>
        <span>{label}</span>
        {sum.counted && (
          <>
            <i data-io="up">+{tokens(sum.insertions)}</i>
            <i data-io="down">−{tokens(sum.deletions)}</i>
          </>
        )}
      </button>
    </div>
  );
}
