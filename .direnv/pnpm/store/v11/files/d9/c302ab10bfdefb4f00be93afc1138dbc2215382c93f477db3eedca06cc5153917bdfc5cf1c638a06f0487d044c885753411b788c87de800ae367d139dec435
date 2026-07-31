/**
 * Component to render markdown.
 *
 * This is a synchronous component.
 * When using async plugins,
 * see {@linkcode MarkdownAsync} or {@linkcode MarkdownHooks}.
 *
 * @param {Readonly<Options>} options
 *   Props.
 * @returns {ReactElement}
 *   React element.
 */
export function Markdown(options: Readonly<Options>): ReactElement;
/**
 * Component to render markdown with support for async plugins
 * through async/await.
 *
 * Components returning promises are supported on the server.
 * For async support on the client,
 * see {@linkcode MarkdownHooks}.
 *
 * @param {Readonly<Options>} options
 *   Props.
 * @returns {Promise<ReactElement>}
 *   Promise to a React element.
 */
export function MarkdownAsync(options: Readonly<Options>): Promise<ReactElement>;
/**
 * Component to render markdown with support for async plugins through hooks.
 *
 * This uses `useEffect` and `useState` hooks.
 * Hooks run on the client and do not immediately render something.
 * For async support on the server,
 * see {@linkcode MarkdownAsync}.
 *
 * @param {Readonly<HooksOptions>} options
 *   Props.
 * @returns {ReactNode}
 *   React node.
 */
export function MarkdownHooks(options: Readonly<HooksOptions>): ReactNode;
/**
 * Make a URL safe.
 *
 * @satisfies {UrlTransform}
 * @param {string} value
 *   URL.
 * @returns {string}
 *   Safe URL.
 */
export function defaultUrlTransform(value: string): string;
/**
 * Filter elements.
 */
export type AllowElement = (element: Readonly<Element>, index: number, parent: Readonly<Parents> | undefined) => boolean | null | undefined;
/**
 * Extra fields we pass.
 */
export type ExtraProps = {
    /**
     * passed when `passNode` is on.
     */
    node?: Element | undefined;
};
/**
 * Map tag names to components.
 */
export type Components = { [Key in keyof JSX.IntrinsicElements]?: ComponentType<JSX.IntrinsicElements[Key] & ExtraProps> | keyof JSX.IntrinsicElements; };
/**
 * Deprecation.
 */
export type Deprecation = {
    /**
     *   Old field.
     */
    from: string;
    /**
     *   ID in readme.
     */
    id: string;
    /**
     * New field.
     */
    to?: keyof Options;
};
/**
 * Configuration.
 */
export type Options = {
    /**
     * Filter elements (optional);
     * `allowedElements` / `disallowedElements` is used first.
     */
    allowElement?: AllowElement | null | undefined;
    /**
     * Tag names to allow (default: all tag names);
     * cannot combine w/ `disallowedElements`.
     */
    allowedElements?: ReadonlyArray<string> | null | undefined;
    /**
     * Markdown.
     */
    children?: string | null | undefined;
    /**
     * Map tag names to components.
     */
    components?: Components | null | undefined;
    /**
     * Tag names to disallow (default: `[]`);
     * cannot combine w/ `allowedElements`.
     */
    disallowedElements?: ReadonlyArray<string> | null | undefined;
    /**
     * List of rehype plugins to use.
     */
    rehypePlugins?: PluggableList | null | undefined;
    /**
     * List of remark plugins to use.
     */
    remarkPlugins?: PluggableList | null | undefined;
    /**
     * Options to pass through to `remark-rehype`.
     */
    remarkRehypeOptions?: Readonly<RemarkRehypeOptions> | null | undefined;
    /**
     * Ignore HTML in markdown completely (default: `false`).
     */
    skipHtml?: boolean | null | undefined;
    /**
     * Extract (unwrap) what’s in disallowed elements (default: `false`);
     * normally when say `strong` is not allowed, it and it’s children are dropped,
     * with `unwrapDisallowed` the element itself is replaced by its children.
     */
    unwrapDisallowed?: boolean | null | undefined;
    /**
     * Change URLs (default: `defaultUrlTransform`)
     */
    urlTransform?: UrlTransform | null | undefined;
};
/**
 * Configuration specifically for {@linkcode MarkdownHooks}.
 */
export type HooksOptionsOnly = {
    /**
     * Content to render while the processor processing the markdown (optional).
     */
    fallback?: ReactNode | null | undefined;
};
/**
 * Configuration for {@linkcode MarkdownHooks};
 * extends the regular {@linkcode Options} with a `fallback` prop.
 */
export type HooksOptions = Options & HooksOptionsOnly;
/**
 * Transform all URLs.
 */
export type UrlTransform = (url: string, key: string, node: Readonly<Element>) => string | null | undefined;
import type { ReactElement } from 'react';
import type { ReactNode } from 'react';
import type { Element } from 'hast';
import type { Parents } from 'hast';
import type { JSX } from 'react';
import type { ComponentType } from 'react';
import type { PluggableList } from 'unified';
import type { Options as RemarkRehypeOptions } from 'remark-rehype';
//# sourceMappingURL=index.d.ts.map