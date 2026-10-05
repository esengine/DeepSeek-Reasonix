import type { WorkspaceChanges } from "../../port/port";
import type { Change } from "./derive";
import { splitPath } from "../args";
import { t } from "../../i18n";

// The filename is the part being read; a deep directory must not push it out
// under an end-ellipsis. Keep the last two segments and mark the elision, with
// the whole path on the row's title for when it matters.
function elide(dir: string): string {
  const parts = dir.split("/").filter(Boolean);
  return parts.length <= 2 ? dir : `…/${parts.slice(-2).join("/")}/`;
}

// The badge is the porcelain letter itself, coloured by what it means; the
// word is kept for the row's title, where there is room to spell it.
const MARK: Record<string, string> = { M: "改过", A: "新增", "??": "新增", D: "已删除", R: "重命名" };

// "??" is two characters wide and every other code is one, so the badge takes
// the first letter and untracked gets the single question mark it reads as.
function badge(status: string): string {
  const s = status.trim();
  return s === "??" ? "?" : s.slice(0, 1);
}

// A rail panel is a read on the run, not a full manifest. A session that edits
// thousands of files would otherwise put a row per file in the DOM — at 4000
// turns that panel alone was 28k nodes, an order of magnitude more than the
// whole transcript. The count in the header stays honest about the total.
const SHOWN = 60;

/** What the rail actually lists, which the overview counts must agree with —
 *  two derivations of "how many files changed" drift the moment one of them
 *  learns about git and the other does not. */
export function visible(changes: Change[], tree: WorkspaceChanges | null) {
  // git is the authority when there is one: a path the session touched but the
  // tree no longer reports has been undone — created then removed, or edited
  // then reverted — and listing it keeps a change pending that does not exist.
  const byPath = tree?.repo ? new Map(tree.changes.map((c) => [c.path, c.status])) : null;
  // git filters, it does not supply: a repository is usually dirty for reasons
  // that have nothing to do with this session, and listing all of it buried the
  // agent's own edits under a page of files at +0 −0.
  return changes
    .filter((f) => !byPath || byPath.has(f.path))
    .map((f) => ({ ...f, status: byPath?.get(f.path) ?? "" }));
}

interface Props {
  changes: Change[];
  yolo: boolean;
  tree: WorkspaceChanges | null;
  // Absent when nothing can be opened — a window with no kernel behind it, or
  // a workspace git cannot answer for. The rows then stay what they were: a
  // readout, with no affordance promising something that would not happen.
  open?: string | null;
  onOpen?: (path: string) => void;
}

export function Files({ changes, yolo, tree, open, onOpen }: Props) {
  const files = visible(changes, tree);
  // Nothing here outlives an unchanged tree: the count is zero and the list is
  // the sentence "尚无改动". A block whose whole content is that it has none is
  // the same standing zero the rest of this rail stopped drawing.
  if (files.length === 0) return null;
  // The tail is the recent end: early files in a long run have been looked at
  // already, and the ones still moving are the ones worth a row.
  const shown = files.length > SHOWN ? files.slice(-SHOWN) : files;

  return (
    <div className="block" data-b="files">
      <div className="lbl">
        <span>{t("工作树改动")}</span>
        <span className="c">
          {files.length}
          {files.length > 0 && ` · ${t(yolo ? "已放行" : "待审")}`}
        </span>
      </div>
      <div className="files">
        {shown.length < files.length && (
          <span className="empty">{t("更早的 {n} 个未列出", { n: files.length - shown.length })}</span>
        )}
        {shown.map((f) => {
          const [dir, name] = splitPath(f.path);
          const mk = badge(f.status);
          const word = MARK[f.status] ?? "";
          const title = [f.path, word && t(word), f.edits > 1 ? t("改了 {n} 次", { n: f.edits }) : ""]
            .filter(Boolean)
            .join(" · ");
          const body = (
            <>
              {mk && <em className="st" data-s={mk}>{mk}</em>}
              <span className="p">
                {dir && <span className="d">{elide(dir)}</span>}
                <span className="f">{name}</span>
              </span>
              <span className="n">
                <span className="a">+{f.added}</span> <span className="r">−{f.removed}</span>
              </span>
            </>
          );
          return onOpen ? (
            <button className="file" key={f.path} type="button" title={title}
              data-on={f.path === open ? "" : undefined} aria-pressed={f.path === open}
              onClick={() => onOpen(f.path)}>
              {body}
            </button>
          ) : (
            <div className="file" key={f.path} title={title}>{body}</div>
          );
        })}
      </div>
    </div>
  );
}
