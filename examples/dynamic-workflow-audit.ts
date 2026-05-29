import { runWorkflow } from "../src/workflow/runtime.js";

const script = `
export const meta = {
  name: "repo_audit",
  description: "Plan a repository audit with fan-out review agents",
  phases: [{ title: "Inventory" }, { title: "Review" }, { title: "Synthesis" }],
}

phase("Inventory")
const inventory = await agent("Map the repository structure and key modules.", {
  label: "repo inventory",
  type: "explore",
})

phase("Review")
const reviews = await parallel([
  () => agent("Review architecture risks:\\n" + inventory, { label: "architecture review" }),
  () => agent("Review security-sensitive paths:\\n" + inventory, { label: "security review" }),
])

phase("Synthesis")
const synthesis = await agent("Synthesize these findings:\\n" + JSON.stringify(reviews), {
  label: "final synthesis",
  type: "synthesis",
})

return { inventory, reviews, synthesis }
`;

const result = await runWorkflow(script, { mode: "dry_run", cwd: process.cwd() });
console.log(JSON.stringify(result, null, 2));
