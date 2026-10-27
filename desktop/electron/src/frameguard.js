// Extension frames run in an opaque-origin sandbox under one kernel route. The
// shell keeps them on that route and keeps every subframe away from its own
// authority: IPC, permissions and navigation.

// The route a runtime serves, optionally under the hub's /rt/<id> mount.
const FRAME_PATH = /^(?:\/rt\/[^/]+)?\/extensions\/frames\/[^/]+\/[^/]+$/;

/** isFrameUrl says whether url is an extension frame document on origin. */
function isFrameUrl(origin, url) {
  if (typeof url !== "string" || origin === "") return false;
  try {
    const u = new URL(url);
    return u.origin === new URL(origin).origin && FRAME_PATH.test(u.pathname);
  } catch {
    return false;
  }
}

/** fromMainFrame answers the window when the IPC came from its top document,
 *  and null otherwise: a subframe never speaks for the window. */
function fromMainFrame(win, event) {
  if (!win || win.isDestroyed() || event.sender !== win.webContents) return null;
  return event.senderFrame && event.senderFrame === win.webContents.mainFrame ? win : null;
}

/** frameNavigationAllowed decides one subframe navigation. A frame that holds
 *  an extension document may only load another extension document; a frame
 *  being created may load one. Other subframes keep their own rules. */
function frameNavigationAllowed(origin, from, to) {
  if (isFrameUrl(origin, from)) return isFrameUrl(origin, to);
  return true;
}

/** guardFrames installs the subframe navigation rule on one WebContents. */
function guardFrames(contents, originOf) {
  contents.on("will-frame-navigate", (event) => {
    if (event.isMainFrame) return;
    if (!frameNavigationAllowed(originOf(), event.frame?.url ?? "", event.url)) event.preventDefault();
  });
}

/** guardPermissions keeps every permission away from subframes. The top
 *  document keeps the platform's default answer. */
function guardPermissions(ses) {
  ses.setPermissionRequestHandler((_contents, _permission, done, details) => done(details?.isMainFrame === true));
  ses.setPermissionCheckHandler((_contents, _permission, _origin, details) => details?.isMainFrame === true);
}

module.exports = { isFrameUrl, fromMainFrame, frameNavigationAllowed, guardFrames, guardPermissions };
