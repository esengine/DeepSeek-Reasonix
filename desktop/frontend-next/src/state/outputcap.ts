import type { Tool } from "../port/wire";

/** A finished call's output longer than this is kept as its two ends. The kernel
 *  already bounds a result for the model; this bounds what the page holds of it. */
export const OUTPUT_CAP = 16 * 1024;
export const OUTPUT_EDGE = 4 * 1024;

// The calls whose output something other than the output card reads: a plan, a
// remembered fact, a diff drawn as a diff, and the lookups whose line and hit
// counts the manifest card counts from the text.
const WHOLE = new Set(["todo_write", "remember", "update_goal", "read_file", "grep", "glob", "ls"]);

/** What the page keeps of a tool: the same call with `hidden` counting the
 *  characters of the middle of its output that it let go of. */
export type StateTool = Tool & { hidden?: number; historyId?: string };

export function capToolOutput(tool: StateTool): StateTool {
  const out = tool.output;
  if (!out || out.length <= OUTPUT_CAP || tool.outputDiff || tool.diff || WHOLE.has(tool.name)) return tool;
  let head = OUTPUT_EDGE;
  let tail = out.length - OUTPUT_EDGE;
  // A cut between the halves of a surrogate pair would end or start an end on half a character.
  if (isHigh(out.charCodeAt(head - 1))) head--;
  if (isLow(out.charCodeAt(tail))) tail++;
  return { ...tool, output: out.slice(0, head) + "\n⋯\n" + out.slice(tail), hidden: tail - head };
}

const isHigh = (c: number) => c >= 0xd800 && c <= 0xdbff;
const isLow = (c: number) => c >= 0xdc00 && c <= 0xdfff;
