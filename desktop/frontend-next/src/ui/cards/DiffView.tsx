import { Fragment, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { RewindPlan, RewindResult } from "../../port/port";

interface Props {
  diff: string;
  path?: string;
  /** The surface around this diff already names the file. */
  named?: boolean;
  onPrepare?: (path: string) => Promise<RewindPlan>;
  onCommit?: (planId: string, resolution?: string) => Promise<RewindResult>;
}

// Reverting is two steps on purpose. The first answers "is this file still the
// one the checkpoint captured"; only when it is not does the second need an
// answer from the reader, and asking before knowing would ask every time.

type Row = { sign: " " | "+" | "-"; text: string; no: number | null } | { skipped: number };

// Unified diff 的行号住在 @@ 头里。此前整段按 \n 切开、序号直接用下标，于是一处
// 改在第 205 行的改动在卡上标成 1、2、3 —— 而那个头本身还被当成代码渲染了出来。
// 空行也不再丢：缩进块的形状就是靠它们读出来的。
export function parseDiff(diff: string): Row[] {
  const out: Row[] = [];
  let oldNo = 0;
  let newNo = 0;
  let lastNew = 0;
  let sawHunk = false;
  // A trailing newline is the diff's terminator, not an empty row: splitting on
  // it left a blank numbered row under every card.
  const lines = diff.replace(/\n+$/, "").split("\n");
  // A `git show` / `git log -p` result leads with a commit header and its
  // indented message; the rows are the diff's, so start at the first file.
  const from = /^commit \S/.test(lines[0] ?? "")
    ? Math.max(0, lines.findIndex((l) => l.startsWith("diff --git ") || l.startsWith("--- ")))
    : 0;
  for (const raw of lines.slice(from)) {
    const at = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(raw);
    if (at) {
      const start = Number(at[2]);
      if (sawHunk && start > lastNew + 1) out.push({ skipped: start - lastNew - 1 });
      oldNo = Number(at[1]);
      newNo = start;
      sawHunk = true;
      continue;
    }
    // 文件头不是内容，从来都不该出现在正文里。
    if (/^(diff |index |--- |\+\+\+ |new file|deleted file|similarity index|rename )/.test(raw)) continue;
    const c = raw[0];
    const sign: " " | "+" | "-" = c === "+" || c === "-" ? c : " ";
    const text = sign === " " ? raw : raw.slice(1);
    if (!sawHunk) {
      // 没有 @@ 头的裸片段：给不出真行号，就不给 —— 编一个比留空更坏。
      out.push({ sign, text, no: null });
      continue;
    }
    if (sign === "+") {
      out.push({ sign, text, no: newNo });
      lastNew = newNo++;
    } else if (sign === "-") {
      // 删掉的那一行在改动后的文件里不存在，所以这一格是空的。行号列自始至终
      // 是同一件事：跳过去要落在第几行。混进旧文件的行号，两种数字长得一样，
      // 读者没有办法分辨自己看的是哪一个文件。
      out.push({ sign, text, no: null });
      oldNo++;
    } else {
      out.push({ sign, text, no: newNo });
      lastNew = newNo++;
      oldNo++;
    }
  }
  return out;
}

// pathFromDiff reads the file a diff names, for a card that has no tool args to
// take it from: the first file's "+++ b/…" (falling back to "--- a/…" for a
// deletion, whose new side is /dev/null).
export function pathFromDiff(diff: string): string | undefined {
  let oldPath: string | undefined;
  for (const line of diff.split("\n")) {
    const plus = /^\+\+\+ (?:b\/)?(\S+)/.exec(line);
    if (plus && plus[1] !== "/dev/null") return plus[1];
    const minus = /^--- (?:a\/)?(\S+)/.exec(line);
    if (minus && minus[1] !== "/dev/null") oldPath ??= minus[1];
  }
  return oldPath;
}

// One file's worth of a diff: the name it carries (when it has a file header),
// its +/- tally, and its rows. Mirrors the CLI's per-file sections
// (termrender/md.go), so a multi-file diff names every file, not just the first.
export type Section = { path?: string; added: number; removed: number; rows: Row[] };

// splitDiffSections cuts a diff into per-file sections by the same rule the CLI
// uses: a git-style diff splits on "diff --git ", anything else on the
// "--- "/"+++ " pair — with the lookahead so a removed "-- x" line (rendered
// "--- x") cannot start a section on its own.
export function splitDiffSections(diff: string): string[] {
  const lines = diff.replace(/\n+$/, "").split("\n");
  const gitStyle = lines.some((l) => l.startsWith("diff --git "));
  const sections: string[] = [];
  let cur: string[] = [];
  lines.forEach((ln, i) => {
    if (cur.length > 0 && diffSectionStart(lines, i, gitStyle)) {
      sections.push(cur.join("\n"));
      cur = [];
    }
    cur.push(ln);
  });
  if (cur.length > 0) sections.push(cur.join("\n"));
  return sections;
}

function diffSectionStart(lines: string[], i: number, gitStyle: boolean): boolean {
  if (gitStyle) return lines[i].startsWith("diff --git ");
  return lines[i].startsWith("--- ") && i + 1 < lines.length && lines[i + 1].startsWith("+++ ");
}

// sectionPath names a section's file from its "+++ b/…" (falling back to
// "--- a/…" for a deletion, whose new side is /dev/null). Undefined for a
// section with no file header — a bare "@@ …" hunk, or a `git show` preamble.
function sectionPath(section: string): string | undefined {
  let oldPath: string | undefined;
  for (const line of section.split("\n")) {
    const plus = /^\+\+\+ (?:b\/)?(\S+)/.exec(line);
    if (plus && plus[1] !== "/dev/null") return plus[1];
    const minus = /^--- (?:a\/)?(\S+)/.exec(line);
    if (minus && minus[1] !== "/dev/null") oldPath ??= minus[1];
  }
  return oldPath;
}

// hasDiffBody reports whether a section carries diff content (a "@@ …" hunk or
// an added/removed line). A `git show` / `git log -p` commit header does not, so
// its section is dropped rather than parsed as context rows.
function hasDiffBody(section: string): boolean {
  return section.split("\n").some((l) => l.startsWith("@@ ") || l.startsWith("+") || l.startsWith("-"));
}

// countSection tallies a section's added/removed rows for its header stat, using
// the same positional header-pair drop as parseDiff.
function countSection(section: string): { added: number; removed: number } {
  const lines = section.split("\n");
  if (lines.length >= 2 && lines[0].startsWith("--- ") && lines[1].startsWith("+++ ")) lines.splice(0, 2);
  let added = 0;
  let removed = 0;
  for (const ln of lines) {
    if (ln.startsWith("+++ ") || ln.startsWith("--- ")) continue;
    if (ln.startsWith("+")) added++;
    else if (ln.startsWith("-")) removed++;
  }
  return { added, removed };
}

// parseDiffSections splits a diff into files, each with its own rows and header
// facts. A section that is only a commit preamble (no file header, no diff
// content) is dropped.
export function parseDiffSections(diff: string): Section[] {
  const out: Section[] = [];
  for (const sec of splitDiffSections(diff)) {
    const path = sectionPath(sec);
    if (path === undefined && !hasDiffBody(sec)) continue;
    out.push({ path, ...countSection(sec), rows: parseDiff(sec) });
  }
  return out;
}

export function DiffView({ diff, path, named, onPrepare, onCommit }: Props) {
  const sections = parseDiffSections(diff);
  // A multi-file diff names each file in its own body header, so the headline
  // would only repeat the first; a single-file diff has no body header, so the
  // headline stays its only name.
  const multi = sections.length > 1;
  // A host-tagged shell diff carries no tool args to name its file, so read the
  // first name off the diff itself when the caller passes no path.
  const shown = path ?? pathFromDiff(diff);
  const [plan, setPlan] = useState<RewindPlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<"" | "reverted" | "kept" | "refused">("");
  const [failed, setFailed] = useState("");
  const revertable = Boolean(path && onPrepare && onCommit && !outcome);
  const clash = plan?.conflicts?.[0];

  const prepare = async () => {
    if (!path || !onPrepare) return;
    setBusy(true);
    setFailed("");
    try {
      const got = await onPrepare(path);
      // The kernel refuses some files outright — not session-owned, payload
      // expired, path unsafe — and says so on the plan. Offering the confirm
      // anyway posts an empty planId and answers "missing planId".
      if (!got.canFiles || !got.planId) {
        setOutcome("refused");
        setFailed(got.disabledReason || t("该文件无法从检查点还原"));
        return;
      }
      setPlan(got);
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = async (resolution?: string) => {
    if (!plan || !onCommit) return;
    setBusy(true);
    setFailed("");
    try {
      const result = await onCommit(plan.planId, resolution);
      setPlan(null);
      // keep_current is the kernel's deliberate no-op: it answers OK and writes
      // nothing. Reporting that as "reverted" tells the reader the opposite of
      // what they just chose.
      setOutcome(result.written?.length || result.deleted?.length ? "reverted" : "kept");
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="dif">
      <div className="dif-hd">
        {/* The card's own headline already names the file; saying it again
            here truncates one fact twice. Where there is no headline — the
            change preview — this row is the only thing that names it. */}
        <span title={shown ?? undefined}>{named ? "" : (shown ?? t("改动"))}</span>
        {outcome === "reverted" ? (
          <span className="ro">{t("已还原")}</span>
        ) : outcome === "kept" ? (
          <span className="ro">{t("保留了当前版本")}</span>
        ) : revertable && !plan ? (
          <button className="dif-act" data-action="file-revert.prepare" disabled={busy} onClick={() => void prepare()}>
            {t(busy ? "…" : "还原该文件")}
          </button>
        ) : (
          <span className="ro">{t("只读")}</span>
        )}
      </div>

      {plan && (
        <div className="dif-ask" data-clash={clash ? "" : undefined}>
          {clash ? (
            <>
              <div className="q">{t("该文件在检查点之后再次被修改。")}</div>
              <div className="row">
                <button className="dif-act" data-action="file-revert.commit" data-value="overwrite_checkpoint" disabled={busy} onClick={() => void commit("overwrite_checkpoint")}>
                  {t("以检查点版本覆盖")}
                </button>
                <button className="dif-act ghost" data-action="file-revert.commit" data-value="keep_current" disabled={busy} onClick={() => void commit("keep_current")}>
                  {t("保留当前版本")}
                </button>
                <button className="dif-act ghost" onClick={() => setPlan(null)}>{t("取消")}</button>
              </div>
            </>
          ) : (
            <>
              {/* earliestRevisions(0): the preimage is the session's first, not
                  this turn's. Saying "before this change" would promise a
                  surgical undo and deliver a session-wide one. */}
              <div className="q">{t("将该文件还原至本次会话开始时的状态 —— 会话中对它的其他改动也会一并撤销。")}</div>
              <div className="row">
                <button className="dif-act" data-action="file-revert.commit" disabled={busy} onClick={() => void commit()}>
                  {t(busy ? "正在还原…" : "还原")}
                </button>
                <button className="dif-act ghost" onClick={() => setPlan(null)}>{t("取消")}</button>
              </div>
            </>
          )}
        </div>
      )}
      {failed && <div className="dif-ask">{failed}</div>}

      {/* 长行横滚在这一层，不在整块上：行号列跟着滚出去，读者就找不到自己在哪
          一行了。被改的那一段常常正好在行尾。 */}
      <div className="dlwrap">
        {sections.map((sec, si) => (
          <Fragment key={si}>
            {multi && sec.path !== undefined && (
              <div className="dfile">
                <span className="p" title={sec.path}>{sec.path}</span>
                <span className="st">
                  {sec.added > 0 && <span className="add">+{sec.added}</span>}
                  {sec.removed > 0 && <span className="del">-{sec.removed}</span>}
                </span>
              </div>
            )}
            {sec.rows.map((l, i) =>
              "skipped" in l ? (
                <div className="dhunk" key={i}>
                  <span>⋯</span>
                  <span>{t("跳过 {n} 行", { n: l.skipped })}</span>
                </div>
              ) : (
                <div className="dl" key={i} data-d={l.sign === " " ? undefined : l.sign}>
                  <span className="no">{l.no ?? ""}</span>
                  <span className="sg">{l.sign}</span>
                  <span className="cd">{l.text}</span>
                </div>
              ),
            )}
          </Fragment>
        ))}
      </div>
    </div>
  );
}
