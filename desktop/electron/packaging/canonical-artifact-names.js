"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { built, canonical, map } = require("./arch");

// The archs whose electron-builder spelling differs from the release one. A
// name still carrying one of these got past the rename, which is the failure
// this refuses to ship rather than report.
const foreign = Object.keys(map).filter((from) => map[from] !== from);

// plan records the rename of one artifact and of everything written beside it
// under its name — a .blockmap left behind points at a file that no longer
// exists, and it arrives in the artifact list as an entry of its own, so the
// list has to be remapped rather than each entry renamed as it comes.
function plan(file, from, to, into) {
  const dir = path.dirname(file);
  const before = path.basename(file);
  for (const entry of fs.readdirSync(dir)) {
    if (entry !== before && !entry.startsWith(`${before}.`)) continue;
    into.set(path.join(dir, entry), path.join(dir, entry.replace(`-${from}`, `-${to}`)));
  }
}

// afterAllArtifactBuild: electron-builder names artifacts in its own vocabulary
// (x64), and the release vocabulary is GOARCH (amd64) because that is what the
// updater asks runtime for. Translating here keeps every consumer downstream —
// studio-manifest, sign, the updater — knowing only the canonical form.
//
// It is also where the blockmap goes. macOS writes one for every zip with no
// option to turn it off — macPackager builds that target with update-info
// writing hardcoded on — so where nsis takes differentialPackage: false, this
// is the only place left to refuse it. Deleted rather than dropped from the
// returned list: what this hook returns is only what to publish additionally,
// and what the release counts is what is in the directory.
module.exports = async function canonicalArtifactNames(buildResult) {
  const renames = new Map();
  for (const file of buildResult.artifactPaths) {
    const arch = built.get(path.resolve(file));
    if (!arch) continue;
    const to = canonical(arch);
    if (to !== arch) plan(path.resolve(file), arch, to, renames);
  }
  for (const [from, to] of renames) fs.renameSync(from, to);

  const renamed = buildResult.artifactPaths.map((file) => renames.get(path.resolve(file)) ?? file);
  const out = [];
  for (const file of renamed) {
    if (file.endsWith(".blockmap")) {
      fs.rmSync(file, { force: true });
      continue;
    }
    out.push(file);
  }
  for (const file of out) {
    const name = path.basename(file);
    const stray = foreign.find((a) => name.includes(`-${a}.`) || name.includes(`-${a}-`));
    if (stray) {
      throw new Error(`${name} still names ${stray}; studio-manifest reads artifact names as GOARCH`);
    }
  }
  return out;
};
