"use strict";
const path = require("node:path");
const { execFileSync } = require("node:child_process");

// Which launches are the same Studio is Reasonix's question: one data home is
// one Studio, canonicalized where it lives. The kernel answers it, because the
// home is resolved from an environment and a platform default this process
// does not own.
function instanceID(binary) {
  try {
    return execFileSync(binary, ["-instance-id"], { encoding: "utf8", timeout: 10000 }).trim();
  } catch {
    // A kernel that cannot be run at all is a launch that is going to fail with
    // a better message in a moment. Refusing to start here would report it as
    // "already running", which is the wrong thing to tell someone.
    return "";
  }
}

// profileFor puts this instance's browser profile under its own identity, which
// is also what scopes the platform lock: two homes are two Studios and must not
// share either. An unknown identity leaves the default alone.
function profileFor(base, id) {
  return id ? path.join(base, id) : base;
}

module.exports = { instanceID, profileFor };
