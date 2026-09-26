"use strict";
const path = require("node:path");

// A pane's base as the hub publishes it. Nothing else may steer a request this
// process sends with the credential, and the unprefixed root is no pane.
const PANE_BASE = /^\/rt\/[A-Za-z0-9_-]+$/;

function refused(error, code = "") {
  return { code, error };
}

// What this machine accepts as a place to show. On Windows a share or a device
// namespace (\\host\share, \\?\, \??\) is reached just by being selected, so
// only a drive-letter path passes.
function local(target, platform) {
  if (platform !== "win32") return path.posix.isAbsolute(target);
  return /^[A-Za-z]:[\\/]/.test(target);
}

// locate asks the kernel where a workspace entry lives. The page names a pane
// and a workspace-relative path, never a location on disk: the kernel owns the
// root and refuses whatever resolves outside it, and only its answer is shown.
async function locate(client, base, rel, platform) {
  if (!PANE_BASE.test(base)) return { refusal: refused("not a pane this window publishes") };
  const query = rel ? `?path=${encodeURIComponent(rel)}` : "";
  let res;
  try {
    res = await client.request("GET", `${base}/workspace/locate${query}`);
  } catch (err) {
    return { refusal: refused(err.message) };
  }
  let body = null;
  try {
    body = JSON.parse(res.body);
  } catch {
    // Read below as an answer that names nothing.
  }
  if (res.status < 200 || res.status >= 300) {
    return { refusal: body && typeof body.code === "string" ? body : refused(`HTTP ${res.status}`) };
  }
  if (!body || typeof body.path !== "string" || !local(body.path, platform)) {
    return { refusal: refused("the kernel named no location on this machine") };
  }
  return { target: body.path };
}

// reveal selects the entry in its folder, the workspace root included. Nothing
// here is ever opened: a directory can be an application bundle, and opening
// one launches it. null means it was shown.
async function reveal(client, shell, base, rel, platform = process.platform) {
  const found = await locate(client, String(base), String(rel), platform);
  if (found.refusal) return found.refusal;
  shell.showItemInFolder(found.target);
  return null;
}

module.exports = { reveal };
