"use strict";
const { execFileSync } = require("node:child_process");

const SHOWN_PATHS = 5;

// Chromium's sandboxed children exit while loading a DLL whose access entries
// grant one Windows app package, so the window never paints. The kernel removes
// those grants from the tree this executable runs from, and it has to happen
// before Chromium starts its first child — which is why this runs synchronously.
function stripPackageGrants(binary, { platform, packaged, execPath }, run = execFileSync) {
  if (platform !== "win32" || !packaged) return null;
  try {
    const raw = run(binary, ["-strip-package-grants", "-studio-app", execPath], {
      encoding: "utf8",
      timeout: 15000,
      windowsHide: true,
      // The kernel names each object it kept a grant on or could not read there.
      stdio: ["ignore", "pipe", "inherit"],
    });
    return readReport(raw);
  } catch (err) {
    console.error("reasonix-studio: strip-package-grants:", err.message);
    return null;
  }
}

function readReport(raw) {
  const body = JSON.parse(raw);
  if (!Array.isArray(body?.stripped) || !Array.isArray(body?.refused)) {
    throw new Error("the grant report carried no lists");
  }
  const stripped = body.stripped.filter((p) => typeof p === "string");
  const refused = body.refused.map((r) => r?.path).filter((p) => typeof p === "string");
  return { stripped, refused };
}

// What to tell someone whose window died before it painted, when the kernel
// already knows why: grants it found and could not remove. Anything else is a
// cause this shell does not know, and it says nothing rather than guess.
function unpaintedWindowCause(report, locale) {
  if (!report?.refused.length) return null;
  const shown = report.refused.slice(0, SHOWN_PATHS).join("\n");
  const more = report.refused.length - SHOWN_PATHS;
  if (String(locale).toLowerCase().startsWith("zh")) {
    return {
      title: "Reasonix Studio 无法打开窗口",
      detail:
        "Reasonix Studio 加载的文件上有授予某个 Windows 应用包（AppContainer）的访问项，Chromium 的沙箱进程加载这类文件时会退出。" +
        "Studio 尝试移除它们，以下位置被拒绝：\n\n" + shown + (more > 0 ? `\n……另有 ${more} 处` : "") +
        "\n\n以管理员身份运行一次 Reasonix Studio，它会自行移除这些访问项。",
    };
  }
  return {
    title: "Reasonix Studio could not open its window",
    detail:
      "Files Reasonix Studio loads carry access entries granting a specific Windows app package (AppContainer), " +
      "and Chromium's sandboxed processes exit while loading such a file. Studio tried to remove them and was refused for:\n\n" +
      shown + (more > 0 ? `\n…and ${more} more` : "") +
      "\n\nRun Reasonix Studio once as administrator and it will remove them itself.",
  };
}

module.exports = { stripPackageGrants, readReport, unpaintedWindowCause };
