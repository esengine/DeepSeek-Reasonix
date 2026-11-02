import type { MockTeam } from "../src/port/mock_team";

// Seats a team the way a long-lived one looks: N agents, M groups of three,
// and the first group's log L lines long, every row with its own last activity.
export async function seedTeam(team: MockTeam, agents: number, groups: number, lines: number) {
  await team.bootstrap("Chief");
  const ids: string[] = [];
  for (let i = 1; i <= agents; i++) {
    const a = await team.createAgent({ name: `Agent ${i}`, role: i % 3 ? "写手" : "审稿" });
    ids.push(a.agents.at(-1)!.id);
  }
  const start = Date.now() - 86_400_000;
  ids.forEach((id, i) => team.touch(id, start + i * 60_000));
  for (let g = 0; g < groups; g++) {
    const view = await team.createGroup({ name: `群 ${g + 1}`, members: ["a-chief", ids[g % ids.length], ids[(g + 1) % ids.length]] });
    const id = view.groups.at(-1)!.id;
    team.touch(id, start + (agents + g) * 60_000);
    if (g === 0) for (let n = 1; n <= lines; n++) team.addLine(id, { from: ids[n % 2], kind: "post", text: `第 ${n} 行：${"群里的一句话，长短不一。".repeat(1 + (n % 4))}` });
  }
}
