import { Awaitable, IconifyLoaderOptions } from "./types.js";
declare const loaderDefaultWidthProp = "__iconify_loader_width";
declare const loaderDefaultHeightProp = "__iconify_loader_height";
declare function mergeIconProps(svg: string, collection: string, icon: string, options?: IconifyLoaderOptions, propsProvider?: () => Awaitable<Record<string, string>>, afterCustomizations?: (props: Record<string, string>) => void): Promise<string>;
declare function getPossibleIconNames(icon: string): string[];
export { getPossibleIconNames, loaderDefaultHeightProp, loaderDefaultWidthProp, mergeIconProps };