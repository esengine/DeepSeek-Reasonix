export type TreeRow = {
  kind: "folder" | "file";
  path: string;
  name: string;
  depth: number;
};

/** Folders first at every level, with the tree still reading as a tree. Sorting
 *  flat paths as text interleaves `bin/` with `boot.test.exe`, because text is
 *  the only thing it compares. Segment by segment, whichever row is a directory
 *  where the two paths part wins there, and its children stay under it. */
function before(a: TreeRow, b: TreeRow): number {
  const x = a.path.split("/"), y = b.path.split("/");
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    if (x[i] === y[i]) continue;
    const dirs = [i < x.length - 1 || a.kind === "folder", i < y.length - 1 || b.kind === "folder"];
    return dirs[0] === dirs[1] ? x[i].localeCompare(y[i]) : dirs[0] ? -1 : 1;
  }
  return x.length - y.length;
}

export function treeRows(
  files: string[],
  directories: string[],
  query: string,
  closed: Set<string>,
): TreeRow[] {
  const matches = query
    ? files.filter((path) =>
        path.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
      )
    : files;
  const folders = new Set<string>(directories);
  for (const path of matches)
    for (let i = 1, bits = path.split("/"); i < bits.length; i++)
      folders.add(bits.slice(0, i).join("/"));
  const hidden = (path: string) =>
    [...closed].some(
      (folder) => path === folder || path.startsWith(`${folder}/`),
    );
  const rows: TreeRow[] = [...folders]
    .filter(
      (path) => ![...closed].some((parent) => path.startsWith(`${parent}/`)),
    )
    .map((path) => ({
      kind: "folder",
      path,
      name: path.split("/").at(-1)!,
      depth: path.split("/").length - 1,
    }));
  rows.push(
    ...matches
      .filter((path) => !hidden(path.split("/").slice(0, -1).join("/")))
      .map((path) => ({
        kind: "file" as const,
        path,
        name: path.split("/").at(-1)!,
        depth: path.split("/").length - 1,
      })),
  );
  return rows.sort(before);
}
