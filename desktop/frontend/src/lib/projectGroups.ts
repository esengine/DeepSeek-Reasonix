// Project-level grouping: organize projects into user-defined groups (e.g.
// "Work", "Personal") so different project types are easy to manage (#9222).
// A project belongs to at most one group (single-select). Storage is local-only,
// following the existing project-tree localStorage precedent (WORKBENCH_*_KEY).

export interface ProjectGroup {
  id: string;
  title: string;
  sortOrder: number;
}

const GROUPS_KEY = "projectTree:groups";
const ASSIGN_KEY = "projectTree:projectGroupAssign";

let groupSeq = 0;
function nextGroupID(): string {
  groupSeq += 1;
  return `group-${groupSeq}-${Date.now().toString(36)}`;
}

function read<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

function write(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* storage unavailable — grouping degrades to in-memory */
  }
}

export function loadProjectGroups(): ProjectGroup[] {
  const groups = read<ProjectGroup[]>(GROUPS_KEY);
  return Array.isArray(groups) ? groups : [];
}

function persistGroups(groups: ProjectGroup[]): void {
  write(GROUPS_KEY, groups);
}

export function loadProjectGroupAssign(): Record<string, string> {
  const assign = read<Record<string, string>>(ASSIGN_KEY);
  return assign && typeof assign === "object" ? assign : {};
}

function persistAssign(assign: Record<string, string>): void {
  write(ASSIGN_KEY, assign);
}

export function addProjectGroup(title: string, groups: ProjectGroup[]): { groups: ProjectGroup[]; group: ProjectGroup } {
  const next = [...groups];
  const order = next.reduce((max, g) => Math.max(max, g.sortOrder), -1) + 1;
  const group: ProjectGroup = { id: nextGroupID(), title: title.trim() || "Group", sortOrder: order };
  next.push(group);
  persistGroups(next);
  return { groups: next, group };
}

export function renameProjectGroup(groups: ProjectGroup[], id: string, title: string): ProjectGroup[] {
  const next = groups.map((g) => (g.id === id ? { ...g, title: title.trim() || g.title } : g));
  persistGroups(next);
  return next;
}

// deleteProjectGroup removes the group and its project assignments. If
// deleteProjects is true the caller is expected to also remove the projects;
// here we only clear the assignment (projects return to "ungrouped").
export function deleteProjectGroup(
  groups: ProjectGroup[],
  assign: Record<string, string>,
  id: string,
): { groups: ProjectGroup[]; assign: Record<string, string> } {
  const nextGroups = groups.filter((g) => g.id !== id);
  persistGroups(nextGroups);
  const nextAssign: Record<string, string> = {};
  for (const [root, groupId] of Object.entries(assign)) {
    if (groupId !== id) nextAssign[root] = groupId;
  }
  persistAssign(nextAssign);
  return { groups: nextGroups, assign: nextAssign };
}

// moveProjectToGroup assigns a project root to a single group (pass null to
// ungroup). Single-select semantics, per #9222.
export function moveProjectToGroup(
  assign: Record<string, string>,
  root: string,
  groupId: string | null,
): Record<string, string> {
  const next = { ...assign };
  if (groupId === null) {
    delete next[root];
  } else {
    next[root] = groupId;
  }
  persistAssign(next);
  return next;
}

export function groupForProject(assign: Record<string, string>, root?: string): string | null {
  if (!root) return null;
  return assign[root] ?? null;
}
