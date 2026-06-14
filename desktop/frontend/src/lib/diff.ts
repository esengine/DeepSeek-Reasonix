export type DiffRow = {
  type: "ctx" | "add" | "del";
  text: string;
  oldLine?: number;
  newLine?: number;
};

// diffLines is a classic LCS line diff. Used by the diff seam to render edit-tool
// before/after; a real editor (Monaco/CodeMirror merge) would replace the
// rendering, but this keeps the algorithm in one place.
export function diffLines(a: string, b: string): DiffRow[] {
  const x = a.split("\n");
  const y = b.split("\n");
  const n = x.length;
  const m = y.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = x[i] === y[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  let oldLine = 1;
  let newLine = 1;
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      rows.push({ type: "ctx", text: x[i], oldLine, newLine });
      i++;
      j++;
      oldLine++;
      newLine++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      rows.push({ type: "del", text: x[i], oldLine });
      i++;
      oldLine++;
    } else {
      rows.push({ type: "add", text: y[j], newLine });
      j++;
      newLine++;
    }
  }
  while (i < n) {
    rows.push({ type: "del", text: x[i++], oldLine });
    oldLine++;
  }
  while (j < m) {
    rows.push({ type: "add", text: y[j++], newLine });
    newLine++;
  }
  return rows;
}

// cleanGitDiff strips standard git diff headers (diff --git, index, ---, +++)
// and hunk headers (@@ -x,y +x,y @@) so the view focuses directly on the changed lines.
export function cleanGitDiff(diff: string): string {
  const lines = diff.split("\n");
  const cleaned: string[] = [];
  let inHunk = false;

  for (const line of lines) {
    if (line.startsWith("@@ ")) {
      inHunk = true;
      // Skip the @@ line itself, optionally we could keep context if needed,
      // but the user wants pure code changes.
      continue;
    }
    if (inHunk) {
      cleaned.push(line);
    }
  }

  // If no hunks were found (unlikely for a valid diff), fallback to original logic
  if (cleaned.length === 0) {
    const match = diff.match(/^@@\s/m);
    if (match && match.index !== undefined) {
      return diff.slice(match.index).replace(/^@@.*$\n?/gm, "");
    }
    return diff;
  }

  return cleaned.join("\n");
}
