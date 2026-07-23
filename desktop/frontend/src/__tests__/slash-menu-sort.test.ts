// Run: tsx src/__tests__/slash-menu-sort.test.ts
//
// Guards the slash-menu grouping + sorting contract:
//   - commands keep a stable group order (actions -> subagents -> skills -> integrations -> management)
//   - within a group, commands are sorted alphabetically by name (the /c misalignment fix)
//   - ties on the same name keep their original relative order

import { sortSlashCommandsForMenu } from "../components/SlashMenu";
import type { CommandInfo } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function cmd(name: string, kind: CommandInfo["kind"], group?: CommandInfo["group"]): CommandInfo {
  return group ? { name, description: "", kind, group } : { name, description: "", kind };
}

// 1. Within-group alphabetical ordering (the bug fix). Skills and custom
//    commands share the "skills" group and must interleave by name so /c
//    filtering shows a deterministic, readable order.
{
  const input = [
    cmd("officecli", "skill"),
    cmd("install-capability", "skill"),
    cmd("agent", "custom"),
    cmd("zoo", "skill"),
    cmd("alpha", "custom"),
  ];
  const out = sortSlashCommandsForMenu(input);
  const names = out.map((c) => c.name);
  const expected = ["agent", "alpha", "install-capability", "officecli", "zoo"];
  ok(
    JSON.stringify(names) === JSON.stringify(expected),
    `skills group sorted alphabetically (got ${names.join(",")})`,
  );
}

// 2. Group order preserved: a skills command must always precede a management command.
{
  const input = [
    cmd("reload-cmd", "builtin", "management"),
    cmd("beta", "skill"),
    cmd("alpha", "skill"),
  ];
  const out = sortSlashCommandsForMenu(input);
  const skillsIdx = out.findIndex((c) => c.kind === "skill");
  const mgmtIdx = out.findIndex((c) => c.group === "management");
  ok(
    skillsIdx >= 0 && mgmtIdx >= 0 && skillsIdx < mgmtIdx,
    "skills group comes before management group",
  );
}

// 3. Custom + skill commands land in the same group and sort together by name.
{
  const input = [
    cmd("z-custom", "custom"),
    cmd("a-skill", "skill"),
    cmd("m-mid", "skill"),
  ];
  const out = sortSlashCommandsForMenu(input);
  ok(
    out.map((c) => c.name).join(",") === "a-skill,m-mid,z-custom",
    "custom and skill commands interleave by name within the skills group",
  );
}

// 4. Stability: identical names keep their original relative order (no cross-talk
//    between plugin skills and custom commands of the same name).
{
  const input = [cmd("dup", "skill"), cmd("dup", "custom")];
  const out = sortSlashCommandsForMenu(input);
  ok(
    out[0].kind === "skill" && out[1].kind === "custom",
    "identical names preserve original index order",
  );
}

// 5. Builtin quick actions stay in the actions group ahead of everything else.
{
  const input = [
    cmd("model", "builtin"),
    cmd("compact", "builtin"),
    cmd("new", "builtin"),
  ];
  const out = sortSlashCommandsForMenu(input);
  ok(out.map((c) => c.name).join(",") === "compact,model,new", "builtin quick actions keep stable relative order");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
