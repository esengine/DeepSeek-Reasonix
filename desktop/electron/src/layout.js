"use strict";
const path = require("node:path");

// Where the two things this shell launches live. Packaged, both sit in
// resources/ beside app.asar rather than inside it: a child process cannot be
// spawned from an archive, and the kernel serves the SPA off the filesystem.
// Dev reads them from the working tree, which is the layout run-studio.sh fills.
const HOST = "reasonix-studio-host";

function hostBinary({ packaged, resourcesPath, dirname, platform, env = {} }) {
  if (env.REASONIX_STUDIO_HOST) return env.REASONIX_STUDIO_HOST;
  // go build names the binary after the package and adds .exe on Windows, where
  // spawn will not run a file without it.
  const name = platform === "win32" ? `${HOST}.exe` : HOST;
  return path.join(packaged ? resourcesPath : path.join(dirname, ".."), "bin", name);
}

// The helper that operates other applications exists on macOS and Windows. Its
// permissions are the application's that started it, so it runs as this
// shell's grandchild rather than being found anywhere else.
function computerHelper({ packaged, resourcesPath, dirname, platform, env = {} }) {
  if (env.REASONIX_COMPUTER_HELPER) return env.REASONIX_COMPUTER_HELPER;
  if (platform !== "darwin" && platform !== "win32") return "";
  const name = platform === "win32" ? "reasonix-computer-helper.exe" : "reasonix-computer-helper";
  return path.join(packaged ? resourcesPath : path.join(dirname, ".."), "bin", name);
}

function pageDir({ packaged, resourcesPath, dirname, env = {} }) {
  if (env.REASONIX_STUDIO_PAGE) return env.REASONIX_STUDIO_PAGE;
  const root = packaged ? resourcesPath : path.join(dirname, "..", "..");
  return path.join(root, "frontend-next", "dist");
}

module.exports = { hostBinary, computerHelper, pageDir };
