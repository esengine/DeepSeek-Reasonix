import type {
  TrustedHTML,
  TrustedTypesWindow,
} from 'trusted-types/lib/index.js';
import type { Config } from './config';

export interface DOMPurify {
  /**
   * Creates a DOMPurify instance using the given window-like object. Defaults to `window`.
   */
  (root?: WindowLike): DOMPurify;

  /**
   * Version label, exposed for easier checks
   * if DOMPurify is up to date or not
   */
  version: string;

  /**
   * Array of elements that DOMPurify removed during sanitation.
   * Empty if nothing was removed.
   */
  removed: Array<RemovedElement | RemovedAttribute>;

  /**
   * Expose whether this browser supports running the full DOMPurify.
   */
  isSupported: boolean;

  /**
   * Set the configuration once.
   *
   * @param cfg configuration object
   */
  setConfig(cfg?: Config): void;

  /**
   * Removes the configuration.
   */
  clearConfig(): void;

  /**
   * Provides core sanitation functionality.
   *
   * @param dirty string or DOM node
   * @param cfg object
   * @returns Sanitized TrustedHTML.
   */
  sanitize(
    dirty: string | Node,
    cfg: Config & { RETURN_TRUSTED_TYPE: true }
  ): TrustedHTML;

  /**
   * Provides core sanitation functionality.
   *
   * @param dirty DOM node
   * @param cfg object
   * @returns Sanitized DOM node.
   */
  sanitize(dirty: Node, cfg: Config & { IN_PLACE: true }): Node;

  /**
   * Provides core sanitation functionality.
   *
   * @param dirty string or DOM node
   * @param cfg object
   * @returns Sanitized DOM node.
   */
  sanitize(dirty: string | Node, cfg: Config & { RETURN_DOM: true }): Node;

  /**
   * Provides core sanitation functionality.
   *
   * @param dirty string or DOM node
   * @param cfg object
   * @returns Sanitized document fragment.
   */
  sanitize(
    dirty: string | Node,
    cfg: Config & { RETURN_DOM_FRAGMENT: true }
  ): DocumentFragment;

  /**
   * Provides core sanitation functionality.
   *
   * @param dirty string or DOM node
   * @param cfg object
   * @returns Sanitized string.
   */
  sanitize(dirty: string | Node, cfg?: Config): string;

  /**
   * Checks if an attribute value is valid.
   * Uses last set config, if any. Otherwise, uses config defaults.
   *
   * @param tag Tag name of containing element.
   * @param attr Attribute name.
   * @param value Attribute value.
   * @returns Returns true if `value` is valid. Otherwise, returns false.
   */
  isValidAttribute(tag: string, attr: string, value: string): boolean;

  /**
   * Adds a DOMPurify hook.
   *
   * @param entryPoint entry point for the hook to add
   * @param hookFunction function to execute
   */
  addHook(entryPoint: BasicHookName, hookFunction: NodeHook): void;

  /**
   * Adds a DOMPurify hook.
   *
   * @param entryPoint entry point for the hook to add
   * @param hookFunction function to execute
   */
  addHook(entryPoint: ElementHookName, hookFunction: ElementHook): void;

  /**
   * Adds a DOMPurify hook.
   *
   * @param entryPoint entry point for the hook to add
   * @param hookFunction function to execute
   */
  addHook(
    entryPoint: DocumentFragmentHookName,
    hookFunction: DocumentFragmentHook
  ): void;

  /**
   * Adds a DOMPurify hook.
   *
   * @param entryPoint entry point for the hook to add
   * @param hookFunction function to execute
   */
  addHook(
    entryPoint: 'uponSanitizeElement',
    hookFunction: UponSanitizeElementHook
  ): void;

  /**
   * Adds a DOMPurify hook.
   *
   * @param entryPoint entry point for the hook to add
   * @param hookFunction function to execute
   */
  addHook(
    entryPoint: 'uponSanitizeAttribute',
    hookFunction: UponSanitizeAttributeHook
  ): void;

  /**
   * Remove a DOMPurify hook at a given entryPoint
   * (pops it from the stack of hooks if hook not specified)
   *
   * @param entryPoint entry point for the hook to remove
   * @param hookFunction optional specific hook to remove
   * @returns removed hook
   */
  removeHook(
    entryPoint: BasicHookName,
    hookFunction?: NodeHook
  ): NodeHook | undefined;

  /**
   * Remove a DOMPurify hook at a given entryPoint
   * (pops it from the stack of hooks if hook not specified)
   *
   * @param entryPoint entry point for the hook to remove
   * @param hookFunction optional specific hook to remove
   * @returns removed hook
   */
  removeHook(
    entryPoint: ElementHookName,
    hookFunction?: ElementHook
  ): ElementHook | undefined;

  /**
   * Remove a DOMPurify hook at a given entryPoint
   * (pops it from the stack of hooks if hook not specified)
   *
   * @param entryPoint entry point for the hook to remove
   * @param hookFunction optional specific hook to remove
   * @returns removed hook
   */
  removeHook(
    entryPoint: DocumentFragmentHookName,
    hookFunction?: DocumentFragmentHook
  ): DocumentFragmentHook | undefined;

  /**
   * Remove a DOMPurify hook at a given entryPoint
   * (pops it from the stack of hooks if hook not specified)
   *
   * @param entryPoint entry point for the hook to remove
   * @param hookFunction optional specific hook to remove
   * @returns removed hook
   */
  removeHook(
    entryPoint: 'uponSanitizeElement',
    hookFunction?: UponSanitizeElementHook
  ): UponSanitizeElementHook | undefined;

  /**
   * Remove a DOMPurify hook at a given entryPoint
   * (pops it from the stack of hooks if hook not specified)
   *
   * @param entryPoint entry point for the hook to remove
   * @param hookFunction optional specific hook to remove
   * @returns removed hook
   */
  removeHook(
    entryPoint: 'uponSanitizeAttribute',
    hookFunction?: UponSanitizeAttributeHook
  ): UponSanitizeAttributeHook | undefined;

  /**
   * Removes all DOMPurify hooks at a given entryPoint
   *
   * @param entryPoint entry point for the hooks to remove
   */
  removeHooks(entryPoint: HookName): void;

  /**
   * Removes all DOMPurify hooks.
   */
  removeAllHooks(): void;
}

/**
 * An element removed by DOMPurify.
 */
export interface RemovedElement {
  /**
   * The element that was removed.
   */
  element: Node;
}

/**
 * An element removed by DOMPurify.
 */
export interface RemovedAttribute {
  /**
   * The attribute that was removed.
   */
  attribute: Attr | null;

  /**
   * The element that the attribute was removed.
   */
  from: Node;
}

type BasicHookName =
  | 'beforeSanitizeElements'
  | 'afterSanitizeElements'
  | 'uponSanitizeShadowNode';
type ElementHookName = 'beforeSanitizeAttributes' | 'afterSanitizeAttributes';
type DocumentFragmentHookName =
  | 'beforeSanitizeShadowDOM'
  | 'afterSanitizeShadowDOM';
type UponSanitizeElementHookName = 'uponSanitizeElement';
type UponSanitizeAttributeHookName = 'uponSanitizeAttribute';

export interface HooksMap {
  beforeSanitizeElements: NodeHook[];
  afterSanitizeElements: NodeHook[];
  beforeSanitizeShadowDOM: DocumentFragmentHook[];
  uponSanitizeShadowNode: NodeHook[];
  afterSanitizeShadowDOM: DocumentFragmentHook[];
  beforeSanitizeAttributes: ElementHook[];
  afterSanitizeAttributes: ElementHook[];
  uponSanitizeElement: UponSanitizeElementHook[];
  uponSanitizeAttribute: UponSanitizeAttributeHook[];
}

type ArrayElement<T> = T extends Array<infer U> ? U : never;

export type HookFunction = ArrayElement<HooksMap[keyof HooksMap]>;

export type HookName =
  | BasicHookName
  | ElementHookName
  | DocumentFragmentHookName
  | UponSanitizeElementHookName
  | UponSanitizeAttributeHookName;

export type NodeHook = (
  this: DOMPurify,
  currentNode: Node,
  hookEvent: null,
  config: Config
) => void;

export type ElementHook = (
  this: DOMPurify,
  currentNode: Element,
  hookEvent: null,
  config: Config
) => void;

export type DocumentFragmentHook = (
  this: DOMPurify,
  currentNode: DocumentFragment,
  hookEvent: null,
  config: Config
) => void;

export type UponSanitizeElementHook = (
  this: DOMPurify,
  currentNode: Node,
  hookEvent: UponSanitizeElementHookEvent,
  config: Config
) => void;

export type UponSanitizeAttributeHook = (
  this: DOMPurify,
  currentNode: Element,
  hookEvent: UponSanitizeAttributeHookEvent,
  config: Config
) => void;

export interface UponSanitizeElementHookEvent {
  tagName: string;
  allowedTags: Record<string, boolean>;
}

export interface UponSanitizeAttributeHookEvent {
  attrName: string;
  attrValue: string;
  keepAttr: boolean;
  allowedAttributes: Record<string, boolean>;
  forceKeepAttr: boolean | undefined;
}

/**
 * A `Window`-like object containing the properties and types that DOMPurify requires.
 */
export type WindowLike = Pick<
  typeof globalThis,
  | 'DocumentFragment'
  | 'HTMLTemplateElement'
  | 'Node'
  | 'Element'
  | 'NodeFilter'
  | 'NamedNodeMap'
  | 'HTMLFormElement'
  | 'DOMParser'
> & {
  document?: Document;
  MozNamedAttrMap?: typeof window.NamedNodeMap;
} & Pick<TrustedTypesWindow, 'trustedTypes'>;
