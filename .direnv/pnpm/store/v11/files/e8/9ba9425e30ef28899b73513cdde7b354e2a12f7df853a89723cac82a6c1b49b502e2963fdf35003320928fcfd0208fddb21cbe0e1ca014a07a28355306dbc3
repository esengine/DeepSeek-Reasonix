import { IconifyJSON } from "@iconify/types";
/** Parent icons, first is direct parent, then parent of parent and so on. Does not include self */
type ParentIconsList = string[];
/** Result. Key is icon, value is list of parent icons */
type ParentIconsTree = Record<string, ParentIconsList | null>;
/**
 * Resolve icon set icons
 *
 * Returns parent icon for each icon
 */
declare function getIconsTree(data: IconifyJSON, names?: string[]): ParentIconsTree;
export { ParentIconsList, ParentIconsTree, getIconsTree };