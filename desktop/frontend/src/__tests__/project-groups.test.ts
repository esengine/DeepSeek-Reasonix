// Run: tsx src/__tests__/project-groups.test.ts
import { JSDOM } from "jsdom";
import {
  addProjectGroup, deleteProjectGroup, groupForProject, loadProjectGroupAssign, loadProjectGroups,
  moveProjectToGroup, renameProjectGroup,
} from "../lib/projectGroups";
import type { ProjectGroup } from "../lib/projectGroups";

// jsdom provides localStorage
const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost" });
(globalThis as unknown as { localStorage: Storage }).localStorage = dom.window.localStorage;

let passed = 0;
let failed = 0;
function eq(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function main() {
  dom.window.localStorage.clear();

  // add
  let { groups, group } = addProjectGroup("Work", []);
  eq(groups.length, 1, "addProjectGroup creates one group");
  eq(group.title, "Work", "group title set");
  const workId = group.id;
  const res2 = addProjectGroup("Personal", groups);
  groups = res2.groups;
  eq(groups.length, 2, "second group added");
  eq(groups.find((g) => g.title === "Work")?.sortOrder, 0, "first group order 0");
  eq(groups.find((g) => g.title === "Personal")?.sortOrder, 1, "second group order 1");
  const personalId = res2.group.id;

  // rename
  groups = renameProjectGroup(groups, workId, "Client A");
  const renamed: ProjectGroup | undefined = (groups as ProjectGroup[]).find((g) => g.id === workId);
  eq(renamed?.title, "Client A", "renameProjectGroup renames by id");

  // assign (single-select) + move
  let assign: Record<string, string> = {};
  assign = moveProjectToGroup(assign, "/proj/a", workId);
  assign = moveProjectToGroup(assign, "/proj/b", workId);
  assign = moveProjectToGroup(assign, "/proj/c", personalId);
  eq(groupForProject(assign, "/proj/a"), workId, "project a in Work");
  eq(groupForProject(assign, "/proj/b"), workId, "project b in Work");
  eq(groupForProject(assign, "/proj/c"), personalId, "project c in Personal");
  // single-select: moving c to Work replaces its prior group
  assign = moveProjectToGroup(assign, "/proj/c", workId);
  eq(groupForProject(assign, "/proj/c"), workId, "single-select replaces group");
  // ungroup
  assign = moveProjectToGroup(assign, "/proj/a", null);
  eq(groupForProject(assign, "/proj/a"), null, "ungroup clears assignment");

  // delete group clears its assignments (projects return to ungrouped)
  deleteProjectGroup(groups, assign, workId);
  const afterAssign = loadProjectGroupAssign();
  eq(groupForProject(afterAssign, "/proj/b"), null, "deleting the group unassigns its projects");
  eq(loadProjectGroups().some((g) => g.id === personalId), true, "other group survives");

  process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
  if (failed > 0) process.exitCode = 1;
}

void main();
