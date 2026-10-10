"use strict";

// What a typed address is when it names a place on disk. Only a form that cannot
// be a host claims it: a drive letter and a separator, a leading "\\" (also
// "\\?\" and "\\.\"), one leading slash, or a file: URL. "D:foo", "c:",
// "host:8080" and "//host/x" are not paths. A file: URL for another host, or one
// whose path opens with a second slash once escapes and dot segments resolve,
// is a share: Windows reads //host/... as one. internal/platform/browser/
// localpath.go reads the same table (testdata/local_paths.json).
const SPACE = "\\t-\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000\\ufeff";
const TRIM = new RegExp(`^[${SPACE}]+|[${SPACE}]+$`, "g");

function trimTyped(raw) {
  return String(raw ?? "").replace(TRIM, "");
}

function localPath(raw) {
  const text = trimTyped(raw).toWellFormed();
  const flat = text.replace(/[\t\n\r]/g, "");
  if (/^file:\//i.test(flat)) return fileForm(flat.slice(5));
  if (text.startsWith("\\\\?\\") || text.startsWith("\\\\.\\")) return deviceForm(text.slice(4));
  const drive = driveSplit(text);
  if (drive) return { url: `file:///${drive[0]}:/${segments(drive[1], /[\\/]/)}`, kind: "local" };
  if (text.startsWith("\\\\")) return uncForm(text.slice(2));
  if (text.startsWith("/") && !text.startsWith("//")) {
    return { url: `file:///${segments(text.slice(1), /\//)}`, kind: "local" };
  }
  return null;
}

function driveSplit(text) {
  const m = /^\/?([A-Za-z]):[\\/](.*)$/s.exec(text);
  return m ? [m[1], m[2]] : null;
}

function uncForm(rest) {
  const cut = rest.search(/[\\/]/);
  const host = cut < 0 ? rest : rest.slice(0, cut);
  if (!host) return null;
  const share = cut < 0 ? "" : rest.slice(cut + 1);
  return { url: `file://${encodeURIComponent(host)}/${segments(share, /[\\/]/)}`, kind: "network" };
}

function deviceForm(rest) {
  if (/^unc[\\/]/i.test(rest)) return uncForm(rest.slice(4));
  const drive = driveSplit(rest);
  if (drive) return { url: `file:///${drive[0]}:/${segments(drive[1], /[\\/]/)}`, kind: "local" };
  return { url: "", kind: "network" };
}

function fileForm(afterScheme) {
  if (/[\u0000-\u001f\u007f]/.test(afterScheme)) return { url: "", kind: "invalid" };
  let rest = afterScheme.replaceAll("\\", "/");
  let host = "";
  if (rest.startsWith("//")) {
    const cut = rest.indexOf("/", 2);
    host = cut < 0 ? rest.slice(2) : rest.slice(2, cut);
    rest = cut < 0 ? "/" : rest.slice(cut);
  }
  const tailAt = rest.search(/[?#]/);
  let path = tailAt < 0 ? rest : rest.slice(0, tailAt);
  const tail = tailAt < 0 ? "" : rest.slice(tailAt);
  if (/^[A-Za-z]:$/.test(host)) path = "/" + host + path;
  else if (host !== "" && host.toLowerCase() !== "localhost") return { url: "", kind: "network" };
  if (path === "") path = "/";
  if (opensShare(path)) return { url: "", kind: "network" };
  return { url: "file://" + path + tail, kind: "local" };
}

function opensShare(path) {
  const out = [];
  for (const seg of path.replace(/%5c|%2f/gi, "/").replace(/%2e/gi, ".").split("/").slice(1)) {
    if (seg === ".") continue;
    if (seg === "..") out.pop();
    else out.push(seg);
  }
  return out.length > 1 && out[0] === "";
}

function segments(rest, sep) {
  const parts = rest.split(sep).filter(Boolean).map(encodeURIComponent);
  const joined = parts.join("/");
  return parts.length && sep.test(rest.slice(-1)) ? joined + "/" : joined;
}

module.exports = { localPath, trimTyped };
