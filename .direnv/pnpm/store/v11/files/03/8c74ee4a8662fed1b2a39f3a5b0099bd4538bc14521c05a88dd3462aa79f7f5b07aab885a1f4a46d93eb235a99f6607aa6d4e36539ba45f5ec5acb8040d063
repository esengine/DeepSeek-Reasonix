import type { IconifyJSON } from '@iconify/types';
import type { NodeType } from './types.js';
/**
 * Built-in icon pack for treeView nodes.
 *
 * Contains only the two default icons (file and folder), drawn as original
 * shapes for this project. Any other icon must come from a user-registered
 * iconify pack (see `registerIconPacks`) and is referenced from the diagram
 * text as `icon(pack:name)`, or as `icon(name)` together with the
 * `defaultIconPack` config option.
 *
 * Icons use `currentColor` so they can be themed via CSS `color`.
 */
export declare const treeViewIcons: IconifyJSON;
interface IconDetectionConfig {
    /** Exact-filename → icon map used to pick file icons when `showIcons` is enabled */
    filenameIcons?: Record<string, string>;
    /** Extension → icon map (lowercase keys, with or without leading dot) */
    extensionIcons?: Record<string, string>;
}
/**
 * Detect a file-type icon name from the user-configured maps.
 * There is no built-in mapping — without `filenameIcons`/`extensionIcons`
 * config, files get the built-in `file` icon. Filename matches win over
 * extension matches; extensions match case-insensitively.
 * Returns `undefined` when nothing matches.
 */
export declare function detectIcon(name: string, config?: IconDetectionConfig): string | undefined;
/**
 * Resolve the (fully-qualified) iconify reference to render for a node.
 *
 * An explicit `icon()` annotation always wins (with `none` hiding the icon);
 * otherwise, when `showIcons` is enabled, files are matched against the
 * user-configured `filenameIcons`/`extensionIcons` maps, falling back to the
 * built-in file/folder icon. Returns `undefined` when no icon should be
 * rendered.
 */
export declare function getNodeIcon(node: {
    icon?: string;
    name: string;
    nodeType: NodeType;
}, config: {
    showIcons: boolean;
    defaultIconPack: string;
} & IconDetectionConfig): string | undefined;
export {};
