import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { beneath, internalRoots, listPackages, runGoTest } from "./go-test-groups.mjs";

// Packages that carry darwin-tagged or _darwin sources — the only root-module
// code whose behaviour a Linux leg cannot prove. Pull requests run this set so
// the scarce macOS runners are not spent re-running the suite Linux already
// covers; pushes to main-v2 keep the full ./... sweep as the safety net. The
// list is enforced against the tree by macos-go-tests.test.mjs, so a new
// darwin source cannot quietly fall outside the pull-request lane.
export const darwinRoots = internalRoots(
  "cli", "filelock", "fileutil", "mcplaunch", "notify", "projectiondb",
  "repair", "sandbox", "sessioncatalog", "tool/builtin", "workspacelease",
);

export function selectPackages(packages, group) {
  if (!["full", "darwin"].includes(group)) {
    throw new Error(`Unknown macOS test group: ${group}`);
  }
  if (group === "full") return packages;
  return packages.filter(pkg => darwinRoots.some(root => beneath(pkg, root)));
}

export function testArgs(packages, group) {
  const selected = selectPackages(packages, group);
  if (selected.length === 0) throw new Error(`Empty macOS test group: ${group}`);
  return ["test", "-timeout=8m", ...selected];
}

function main(group) {
  const { packages, status } = listPackages();
  if (!packages) return status;
  return runGoTest(`macOS ${group}`, selectPackages(packages, group), testArgs(packages, group));
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = main(process.argv[2]);
}
