"use strict";

const { fileURLToPath } = require("node:url");

// The formats a copied file travels in, and the paths they name.
//
// A file the platform's file manager copied is on the clipboard as a file
// format, not as the text of its path. Those formats are read here so that
// the page can ask what the clipboard carries without ever reading the shape
// of the text: a path copied as text is text, and only a file format on the
// clipboard makes a file.
//
// Electron maps the platform's own "copied files" format onto text/uri-list
// (CF_HDROP on Windows, NSFilenamesPboardType on macOS, text/uri-list on
// Linux), and that payload is a list of file:// URIs. The raw formats behind
// it are read as well, under the names the platform gives them.
const URI_LIST = "text/uri-list";
const osFormat = (name) => `electron application/osclipboard;format="${name}"`;
const FILE_NAME_W = osFormat("FileNameW");
// "FileNameW" carries the path list as raw UTF-16. Some builds expose a bare
// "FileName" instead, but that one arrives in the system ANSI codepage (and as
// an 8.3 short name), so it is not a reliable source and is skipped; the
// Unicode path already comes through text/uri-list, which is what we use.
const PUBLIC_FILE_URL = osFormat("public.file-url");

// A URI list holds one URI per line, and a comment line starts with a hash.
function uriListPaths(text) {
  const paths = [];
  for (const line of String(text ?? "").split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const path = filePath(trimmed);
    if (path) paths.push(path);
  }
  return paths;
}

// The Windows file-name format holds the paths as UTF-16, each ended by a
// null, and the list itself ended by a second null.
function fileNamePaths(bytes) {
  const text = Buffer.from(bytes).toString("utf16le");
  const paths = [];
  for (const part of text.split("\0")) {
    const trimmed = part.trim();
    if (trimmed) paths.push(trimmed);
  }
  return paths;
}

// The macOS file URL holds one file.
function fileUrlPaths(text) {
  const path = filePath(String(text ?? "").trim());
  return path ? [path] : [];
}

// A file URL names a place on disk. Anything else names no file.
function filePath(url) {
  if (!url.toLowerCase().startsWith("file:")) return "";
  try {
    return fileURLToPath(url);
  } catch {
    return "";
  }
}

// The files the clipboard carries, read from the formats that carry files.
// The clipboard that cannot be read answers with nothing rather than with a
// guess.
async function clipboardFiles(clipboard) {
  let items;
  try {
    items = await clipboard.read();
  } catch {
    return [];
  }
  const paths = [];
  for (const item of items) {
    for (const type of item.types) {
      try {
        if (type === URI_LIST) {
          paths.push(...uriListPaths(await payloadText(item, type)));
        } else if (type === FILE_NAME_W) {
          paths.push(...fileNamePaths(await payloadBytes(item, type)));
        } else if (type === PUBLIC_FILE_URL) {
          paths.push(...fileUrlPaths(await payloadText(item, type)));
        }
      } catch {
        // A format that cannot be read is a format that names no file here.
      }
    }
  }
  return [...new Set(paths)];
}

// The payload of a format, as text.
async function payloadText(item, type) {
  const payload = await item.getType(type);
  return typeof payload === "string" ? payload : await payload.text();
}

// The payload of a format, as bytes.
async function payloadBytes(item, type) {
  const payload = await item.getType(type);
  if (typeof payload === "string") return Buffer.from(payload, "utf16le");
  return Buffer.from(await payload.arrayBuffer());
}

module.exports = {
  clipboardFiles,
  fileNamePaths,
  fileUrlPaths,
  uriListPaths,
};
