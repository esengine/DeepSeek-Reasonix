const STORAGE_KEY = "reasonix-full-access-confirmed-projects-v1";

type ConfirmationStore = {
  version: 1;
  projects: string[];
};

function storage(): Storage | undefined {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage;
  } catch {
    return undefined;
  }
}

function readStore(): ConfirmationStore {
  const target = storage();
  if (!target) return { version: 1, projects: [] };
  try {
    const parsed = JSON.parse(target.getItem(STORAGE_KEY) ?? "null") as Partial<ConfirmationStore> | null;
    if (parsed?.version !== 1 || !Array.isArray(parsed.projects)) return { version: 1, projects: [] };
    return {
      version: 1,
      projects: parsed.projects.filter((entry): entry is string => typeof entry === "string" && entry.length > 0),
    };
  } catch {
    return { version: 1, projects: [] };
  }
}

function normalizeWorkspacePath(value: string): string {
  let path = value.trim().replaceAll("\\", "/");
  while (path.length > 1 && path.endsWith("/") && !/^[A-Za-z]:\/$/.test(path)) path = path.slice(0, -1);
  return path;
}

/**
 * Identifies the folder whose Full access warning has been acknowledged.
 * Remote hosts are deliberately isolated even when their workspace paths match.
 */
export function fullAccessProjectConfirmationKey(input: {
  workspacePath?: string;
  remoteHostId?: string;
}): string {
  const workspacePath = normalizeWorkspacePath(input.workspacePath ?? "");
  if (!workspacePath) return "";
  const remoteHostId = input.remoteHostId?.trim();
  return JSON.stringify([remoteHostId ? `remote:${remoteHostId}` : "local", workspacePath]);
}

export function hasConfirmedFullAccessForProject(projectKey: string): boolean {
  return projectKey.length > 0 && readStore().projects.includes(projectKey);
}

export function rememberFullAccessConfirmationForProject(projectKey: string): void {
  if (!projectKey || hasConfirmedFullAccessForProject(projectKey)) return;
  const target = storage();
  if (!target) return;
  const current = readStore();
  try {
    target.setItem(STORAGE_KEY, JSON.stringify({ version: 1, projects: [...current.projects, projectKey] } satisfies ConfirmationStore));
  } catch {
    // Storage is an optional convenience. If it is unavailable, the safe
    // fallback is to show the confirmation again next time.
  }
}
