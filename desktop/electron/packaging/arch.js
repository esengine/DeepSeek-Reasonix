"use strict";
const path = require("node:path");
const { map } = require("../arch.json");

// What each artifact was built for, keyed by the path electron-builder wrote.
// Taken from the Arch enum it hands the hook rather than read back out of the
// name — the name is what this is here to correct.
const built = new Map();

function record(file, arch) {
  built.set(path.resolve(file), arch);
}

// canonical fails closed. A new target arch that nobody mapped would otherwise
// ship as ReasonixStudio-linux-armv7l.deb, which studio-manifest reads as a
// platform no running binary ever asks for — an update nobody is offered, with
// nothing failing to say so.
function canonical(arch) {
  const to = map[arch];
  if (!to) {
    throw new Error(
      `electron-builder built for ${arch}, which arch.json does not map to a release arch. ` +
        `Add it there (and to the studio-manifest gate) or drop the target.`,
    );
  }
  return to;
}

module.exports = { built, record, canonical, map };
