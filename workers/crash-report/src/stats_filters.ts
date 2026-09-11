export type StatsFilters = {
  surface: "desktop" | "cli";
  status: string;
  source: string;
  version: string;
  os: string;
  platform: string;
  osBuild: string;
  osRevision: string;
  distroId: string;
  distroVersion: string;
  kernelVersion: string;
  sessionType: string;
  arch: string;
  channel: string;
  runtimeVersion: string;
  runtimeEngine: string;
  failureKind: string;
  failureReason: string;
  exitCode: string;
  recovery: string;
  gpu: string;
  newLatest: boolean;
  regressed: boolean;
  windowDays: 7 | 30;
};

const limited = (url: URL, name: string, max: number) => (url.searchParams.get(name) ?? "").slice(0, max);
const oneOf = (value: string, choices: readonly string[]) => choices.includes(value) ? value : "";

export function statsFilters(url: URL): StatsFilters {
  const windowParam = limited(url, "window", 3);
  const exitCodeParam = limited(url, "exitCode", 16);
  return {
    surface: limited(url, "surface", 7) === "cli" ? "cli" : "desktop",
    status: oneOf(limited(url, "status", 8), ["open", "resolved", "ignored"]),
    source: limited(url, "source", 32),
    version: limited(url, "version", 64),
    os: limited(url, "os", 32),
    platform: limited(url, "platform", 80),
    osBuild: limited(url, "osBuild", 7).replace(/[^0-9]/g, ""),
    osRevision: limited(url, "osRevision", 7).replace(/[^0-9]/g, ""),
    distroId: limited(url, "distro", 64).replace(/[^a-zA-Z0-9_.-]/g, ""),
    distroVersion: limited(url, "distroVersion", 64).replace(/[^a-zA-Z0-9_.-]/g, ""),
    kernelVersion: limited(url, "kernel", 128).replace(/[^a-zA-Z0-9_.+-]/g, ""),
    sessionType: oneOf(limited(url, "session", 8), ["wayland", "x11", "remote", "unknown"]),
    arch: limited(url, "arch", 32),
    channel: limited(url, "channel", 32),
    runtimeVersion: limited(url, "runtime", 128),
    runtimeEngine: oneOf(limited(url, "engine", 16), ["webview2", "webkitgtk"]),
    failureKind: limited(url, "failureKind", 64),
    failureReason: limited(url, "reason", 64),
    exitCode: exitCodeParam === "unknown" || /^-?\d+$/.test(exitCodeParam) ? exitCodeParam : "",
    recovery: limited(url, "recovery", 32),
    gpu: oneOf(limited(url, "gpu", 16), ["enabled", "disabled", "always", "on_demand", "unknown"]),
    newLatest: limited(url, "new", 6) === "latest",
    regressed: limited(url, "regressed", 1) === "1",
    windowDays: windowParam === "7d" ? 7 : 30,
  };
}
