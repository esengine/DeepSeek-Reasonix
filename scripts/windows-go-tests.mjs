import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const isolatedGroups = ["agent", "boot", "control"];
const smokeRoots = [
  "appidentity", "checkpoint", "cli", "desktoplauncher", "extension/sidecar",
  "filelock", "fileutil", "hook", "instruction", "mcplaunch", "notify", "proc",
  "remote", "repair", "sandbox", "sessioncatalog", "sysproxy", "workspacelease",
].map(name => `reasonix/internal/${name}`).concat("reasonix/cmd");
const beneath = (pkg, root) => pkg === root || pkg.startsWith(`${root}/`);

export function selectPackages(packages, group) {
  if (!["full", "smoke", ...isolatedGroups].includes(group)) {
    throw new Error(`Unknown Windows test group: ${group}`);
  }
  const isolated = pkg => isolatedGroups.some(name => beneath(pkg, `reasonix/internal/${name}`));
  return packages.filter(pkg => {
    if (isolatedGroups.includes(group)) return beneath(pkg, `reasonix/internal/${group}`);
    return !isolated(pkg) && (group === "full" || smokeRoots.some(root => beneath(pkg, root)));
  });
}

export function testArgs(packages, group) {
  const selected = selectPackages(packages, group);
  if (selected.length === 0) throw new Error(`Empty Windows test group: ${group}`);
  return ["test", "-p", isolatedGroups.includes(group) ? "1" : "4", "-timeout=8m", ...selected];
}

function main(group) {
  const listed = spawnSync("go", ["list", "./..."], { encoding: "utf8" });
  if (listed.error) throw listed.error;
  if (listed.status !== 0) {
    process.stderr.write(listed.stderr || "go list failed\n");
    return listed.status ?? 1;
  }
  const packages = listed.stdout.trim().split(/\r?\n/).filter(Boolean);
  const args = testArgs(packages, group);
  console.log(`Windows ${group}: ${args.length - 4} packages; go ${args.join(" ")}`);
  const result = spawnSync("go", args, { stdio: "inherit" });
  if (result.error) throw result.error;
  return result.status ?? 1;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = main(process.argv[2]);
}
