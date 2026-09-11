export type WorkspaceChangeTreeInput = { path: string; key?: string };

export type WorkspaceChangeTreeNode<T extends WorkspaceChangeTreeInput> =
  | {
      kind: "folder";
      key: string;
      name: string;
      path: string;
      children: WorkspaceChangeTreeNode<T>[];
    }
  | {
      kind: "file";
      key: string;
      name: string;
      path: string;
      change: T;
    };

function compareNodes<T extends WorkspaceChangeTreeInput>(
  left: WorkspaceChangeTreeNode<T>,
  right: WorkspaceChangeTreeNode<T>,
): number {
  if (left.kind !== right.kind) return left.kind === "folder" ? -1 : 1;
  return left.name.localeCompare(right.name);
}

/** Convert slash-delimited changed paths into a stable folder/file tree. */
export function buildWorkspaceChangeTree<T extends WorkspaceChangeTreeInput>(
  changes: readonly T[],
): WorkspaceChangeTreeNode<T>[] {
  const root: WorkspaceChangeTreeNode<T>[] = [];
  const fileKeyCounts = new Map<string, number>();

  for (const change of changes) {
    const parts = change.path.split("/").filter(Boolean);
    if (parts.length === 0) continue;

    let children = root;
    let folderPath = "";
    for (const part of parts.slice(0, -1)) {
      folderPath += `${part}/`;
      let folder = children.find((node) => node.kind === "folder" && node.path === folderPath);
      if (!folder || folder.kind !== "folder") {
        folder = { kind: "folder", key: `folder:${folderPath}`, name: part, path: folderPath, children: [] };
        children.push(folder);
      }
      children = folder.children;
    }

    const name = parts[parts.length - 1];
    const fileKeyBase = change.key?.trim() || change.path;
    const occurrence = fileKeyCounts.get(fileKeyBase) ?? 0;
    fileKeyCounts.set(fileKeyBase, occurrence + 1);
    const fileKeySuffix = occurrence === 0 ? "" : `:${occurrence}`;
    children.push({ kind: "file", key: `file:${fileKeyBase}${fileKeySuffix}`, name, path: change.path, change });
  }

  const sortTree = (nodes: WorkspaceChangeTreeNode<T>[]) => {
    nodes.sort(compareNodes);
    for (const node of nodes) {
      if (node.kind === "folder") sortTree(node.children);
    }
  };
  sortTree(root);
  return root;
}
