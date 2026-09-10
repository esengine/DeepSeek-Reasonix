import { isAbsolute, normalize, resolve, sep } from "node:path";

export interface HomeEnvironment {
  env: NodeJS.ProcessEnv;
  platform: NodeJS.Platform;
  homedir(): string;
  cwd(): string;
}

const VAR_REF = /\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}/g;

// path.normalize keeps a trailing separator; Go's filepath.Clean does not.
function clean(path: string): string {
  const normalized = normalize(path);
  return normalized.length > 1 && normalized.endsWith(sep) && !/^[A-Za-z]:\\$/.test(normalized) ? normalized.slice(0, -1) : normalized;
}

function expandVars(value: string, env: NodeJS.ProcessEnv): string {
  if (!value.includes("${")) return value;
  return value.replace(VAR_REF, (_match, name: string, fallback: string | undefined) => {
    const found = env[name];
    if (found) return found;
    return fallback ?? "";
  });
}

function userHome(input: HomeEnvironment): string {
  const fromEnv = input.platform === "win32" ? input.env.USERPROFILE : input.env.HOME;
  if (fromEnv && fromEnv.trim() !== "") return fromEnv;
  try {
    return input.homedir();
  } catch {
    return "";
  }
}

function cleanEnvDir(input: HomeEnvironment, name: string): string {
  let dir = (input.env[name] ?? "").trim();
  if (dir === "") return "";
  dir = expandVars(dir, input.env);
  if (dir === "~") {
    const home = userHome(input);
    if (home !== "") dir = home;
  } else if (dir.startsWith("~/") || dir.startsWith("~\\")) {
    const home = userHome(input);
    if (home !== "") dir = home + sep + dir.slice(2);
  }
  if (!isAbsolute(dir)) dir = resolve(input.cwd(), dir);
  return clean(dir);
}

// Mirrors internal/config.ReasonixHomeDir so the hello `instance.home` matches
// what the Go service computes for the same environment.
export function reasonixHome(input: HomeEnvironment): string {
  const explicit = cleanEnvDir(input, "REASONIX_HOME");
  if (explicit !== "") return explicit;
  const home = userHome(input);
  if (input.platform === "win32") {
    const appData = (input.env.APPDATA ?? "").trim();
    if (appData !== "") return clean(appData + sep + "reasonix");
    if (home !== "") return clean(home + sep + "AppData" + sep + "Roaming" + sep + "reasonix");
    return "";
  }
  if (home !== "") return clean(home + sep + ".reasonix");
  const xdg = (input.env.XDG_CONFIG_HOME ?? "").trim();
  if (xdg !== "") return clean(xdg + sep + "reasonix");
  return "";
}
