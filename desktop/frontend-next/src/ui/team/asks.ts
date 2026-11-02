import type { Tool } from "../../port/wire";

// A call whose card asks the person for something: it stays in view, never
// folded into the turn's work, since its answer is owed by whoever reads it.
export function asksThePerson(tool: Pick<Tool, "name">): boolean {
  return tool.name === "request_secret";
}
