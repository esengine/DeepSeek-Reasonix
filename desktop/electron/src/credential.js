"use strict";

// The kernel's gate answers every request without the launch credential with
// 403, and a cookie belongs to the host, not the port, so anything that removes it (an
// eviction, a page clearing cookies for 127.0.0.1) leaves the window refused
// until a relaunch. The shell holds the token, so it puts the cookie back.
function keepCredential(ses, cookie, log) {
  const host = new URL(cookie.url).hostname;
  const restore = (cause) => {
    log(`credential cookie removed (${cause}); restoring`);
    ses.cookies.set(cookie).catch((err) => log(`credential cookie restore failed: ${err.message}`));
  };
  const onChanged = (_event, changed, cause, removed) => {
    if (!removed || changed.name !== cookie.name || cause === "overwrite") return;
    if (changed.domain.replace(/^\./, "") !== host) return;
    restore(cause);
  };
  ses.cookies.on("changed", onChanged);
  return () => ses.cookies.removeListener("changed", onChanged);
}

// The change event is not the only way the jar loses the cookie: a network
// service restart drops session cookies without announcing a removal. A request
// the kernel refused is the moment that shows, so the shell reads the jar then.
async function restoreIfStale(ses, cookie, log) {
  const held = await ses.cookies.get({ url: cookie.url, name: cookie.name });
  const found = held.find((c) => c.name === cookie.name);
  if (found && found.value === cookie.value) return;
  log(`credential cookie ${found ? "differs from this launch's" : "missing"} after a refused request; restoring`);
  await ses.cookies.set(cookie).catch((err) => log(`credential cookie restore failed: ${err.message}`));
}

module.exports = { keepCredential, restoreIfStale };
