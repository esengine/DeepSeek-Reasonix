/** Tool classification used by ACP dispatch and Dashboard cards to assign
 *  consistent icons / colours without hard-coding tool names in the UI layer. */

const READ_TOOLS = new Set([
  "read_file",
  "list_directory",
  "directory_tree",
  "get_file_info",
  "glob",
]);

const EDIT_TOOLS = new Set([
  "write_file",
  "edit_file",
  "multi_edit",
  "create_directory",
  "delete_file",
  "delete_directory",
  "move_file",
  "copy_file",
]);

const SEARCH_TOOLS = new Set(["search_content", "search_files"]);
const EXECUTE_TOOLS = new Set(["run_command", "run_background"]);

export type AcpToolKind = "read" | "edit" | "search" | "execute" | "other";

export function toolKindFor(name: string): AcpToolKind {
  if (READ_TOOLS.has(name)) return "read";
  if (EDIT_TOOLS.has(name)) return "edit";
  if (SEARCH_TOOLS.has(name)) return "search";
  if (EXECUTE_TOOLS.has(name)) return "execute";
  return "other";
}
