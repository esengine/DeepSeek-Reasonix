// A copied image travels as a path the reader can paste back into a turn, and
// the path is written in one shape so that a paste can find the file again:
//
//   a.jpg      in the working folder  →  @//a.jpg/
//   c:\a.jpg   at a given path         →  @//c/:\a.jpg
//
// The drive's colon is separated by a slash, and a working-folder path ends in
// a slash: the two shapes then cannot be mistaken for one another, and each
// reads back as the path it stands for. A working-folder path is relative to
// the folder the turn runs in; the other names a place of its own.

export const IMAGE_PATH_PREFIX = "@//";

// A token is an absolute path — the drive's colon shows up — or a working-folder
// path, which ends in a slash. Three shapes are matched; the POSIX-root form is
// tried before the working-folder form so its interior slashes are not swallowed
// by the trailing-slash rule:
//   @//<drive>/:<...>      an absolute path on a drive letter (c:/a.jpg → c/:/a.jpg)
//   @///<...>             an absolute path on a POSIX root (/a/b.png → ///a/b.png)
//   @//<...>/             a working-folder path, ending in a slash
const imagePathToken =
  /@\/\/[^\s]*\/:[^\s]*|@\/\/\/[^\s]+|@\/\/[^\s]*\//g;

/** Whether a path names a place of its own rather than one inside the working folder. */
export function isAbsolutePath(path: string): boolean {
  return /^[A-Za-z]:[\\/]/.test(path) || path.startsWith("/") || path.startsWith("\\\\");
}

/** Writes a path in the shape a paste reads back. */
export function annotateImagePath(path: string): string {
  const p = path.trim();
  if (!p) return "";
  if (isAbsolutePath(p)) {
    // A drive path keeps its colon as "/:" so the slash reads back as a colon; a
    // POSIX root path keeps its leading slash, which merges with the prefix's
    // own slashes and is undone on the way back out.
    if (/^[A-Za-z]:/.test(p)) return IMAGE_PATH_PREFIX + p.replace(":", "/:");
    return IMAGE_PATH_PREFIX + p;
  }
  return IMAGE_PATH_PREFIX + p + "/";
}

/** Reads the annotated paths out of a pasted text, and the text without them. */
export function parseImagePaths(text: string): { paths: string[]; text: string } {
  const paths: string[] = [];
  const rest = text.replace(imagePathToken, (tok) => {
    const decoded = decodeImagePath(tok);
    if (decoded) paths.push(decoded);
    return "";
  });
  return { paths, text: rest.replace(/[ \t]{2,}/g, " ").trim() };
}

function decodeImagePath(token: string): string {
  const body = token.slice(IMAGE_PATH_PREFIX.length);
  const drive = body.indexOf("/:");
  if (drive >= 0) return body.slice(0, drive) + ":" + body.slice(drive + 2);
  if (body.length > 1 && body.endsWith("/")) return body.slice(0, -1);
  // A POSIX absolute path kept its leading slash, which joined the prefix's own
  // slashes; after the three-character prefix is removed the body is the path.
  if (body.startsWith("/")) return body;
  return body;
}

/** The place a path stands for, as the host has to be given it: a relative path
 *  is relative to the working folder, so it is resolved against it. A root that
 *  is not a place of its own cannot anchor one, and the path is left as it is. */
export function resolveImagePath(path: string, root: string): string {
  if (isAbsolutePath(path) || !root || !isAbsolutePath(root)) return path;
  const sep = root.includes("\\") && !root.includes("/") ? "\\" : "/";
  return root.replace(/[\\/]+$/, "") + sep + path.replace(/^[\\/]+/, "");
}
