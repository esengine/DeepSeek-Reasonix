import type { TrustedTypePolicy } from 'trusted-types/lib/index.js';

/**
 * Configuration to control DOMPurify behavior.
 */
export interface Config {
  /**
   * Extend the existing array of allowed attributes.
   * Can be an array of attribute names, or a function that receives
   * the attribute name and tag name to determine if the attribute is allowed.
   */
  ADD_ATTR?:
    | string[]
    | ((attributeName: string, tagName: string) => boolean)
    | undefined;

  /**
   * Extend the existing array of elements that can use Data URIs.
   */
  ADD_DATA_URI_TAGS?: string[] | undefined;

  /**
   * Extend the existing array of allowed tags.
   * Can be an array of tag names, or a function that receives
   * the tag name to determine if the tag is allowed.
   */
  ADD_TAGS?: string[] | ((tagName: string) => boolean) | undefined;

  /**
   * Extend the existing array of elements that are safe for URI-like values (be careful, XSS risk).
   */
  ADD_URI_SAFE_ATTR?: string[] | undefined;

  /**
   * Allow ARIA attributes, leave other safe HTML as is (default is true).
   */
  ALLOW_ARIA_ATTR?: boolean | undefined;

  /**
   * Allow HTML5 data attributes, leave other safe HTML as is (default is true).
   */
  ALLOW_DATA_ATTR?: boolean | undefined;

  /**
   * Allow external protocol handlers in URL attributes (default is false, be careful, XSS risk).
   * By default only `http`, `https`, `ftp`, `ftps`, `tel`, `mailto`, `callto`, `sms`, `cid` and `xmpp` are allowed.
   */
  ALLOW_UNKNOWN_PROTOCOLS?: boolean | undefined;

  /**
   * Decide if self-closing tags in attributes are allowed.
   * Usually removed due to a mXSS issue in jQuery 3.0.
   */
  ALLOW_SELF_CLOSE_IN_ATTR?: boolean | undefined;

  /**
   * Allow only specific attributes.
   */
  ALLOWED_ATTR?: string[] | undefined;

  /**
   * Allow only specific elements.
   */
  ALLOWED_TAGS?: string[] | undefined;

  /**
   * Allow only specific namespaces. Defaults to:
   *  - `http://www.w3.org/1999/xhtml`
   *  - `http://www.w3.org/2000/svg`
   *  - `http://www.w3.org/1998/Math/MathML`
   */
  ALLOWED_NAMESPACES?: string[] | undefined;

  /**
   * Allow specific protocols handlers in URL attributes via regex (be careful, XSS risk).
   * Default RegExp:
   * ```
   * /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i;
   * ```
   */
  ALLOWED_URI_REGEXP?: RegExp | undefined;

  /**
   * Define how custom elements are handled.
   */
  CUSTOM_ELEMENT_HANDLING?: {
    /**
     * Regular expression or function to match to allowed elements.
     * Default is null (disallow any custom elements).
     */
    tagNameCheck?: RegExp | ((tagName: string) => boolean) | null | undefined;

    /**
     * Regular expression or function to match to allowed attributes.
     * Default is null (disallow any attributes not on the allow list).
     */
    attributeNameCheck?:
      | RegExp
      | ((attributeName: string, tagName?: string) => boolean)
      | null
      | undefined;

    /**
     * Allow custom elements derived from built-ins if they pass `tagNameCheck`. Default is false.
     */
    allowCustomizedBuiltInElements?: boolean | undefined;
  };

  /**
   * Add attributes to block-list.
   */
  FORBID_ATTR?: string[] | undefined;

  /**
   * Add child elements to be removed when their parent is removed.
   */
  FORBID_CONTENTS?: string[] | undefined;

  /**
   * Extend the existing or default array of forbidden content elements.
   */
  ADD_FORBID_CONTENTS?: string[] | undefined;

  /**
   * Add elements to block-list.
   */
  FORBID_TAGS?: string[] | undefined;

  /**
   * Glue elements like style, script or others to `document.body` and prevent unintuitive browser behavior in several edge-cases (default is false).
   */
  FORCE_BODY?: boolean | undefined;

  /**
   * Map of non-standard HTML element names to support. Map to true to enable support. For example:
   *
   * ```
   * HTML_INTEGRATION_POINTS: { foreignobject: true }
   * ```
   */
  HTML_INTEGRATION_POINTS?: Record<string, boolean> | undefined;

  /**
   * Sanitize a node "in place", which is much faster depending on how you use DOMPurify.
   */
  IN_PLACE?: boolean | undefined;

  /**
   * Keep an element's content when the element is removed (default is true).
   */
  KEEP_CONTENT?: boolean | undefined;

  /**
   * Map of MathML element names to support. Map to true to enable support. For example:
   *
   * ```
   * MATHML_TEXT_INTEGRATION_POINTS: { mtext: true }
   * ```
   */
  MATHML_TEXT_INTEGRATION_POINTS?: Record<string, boolean> | undefined;

  /**
   * Change the default namespace from HTML to something different.
   */
  NAMESPACE?: string | undefined;

  /**
   * Change the parser type so sanitized data is treated as XML and not as HTML, which is the default.
   */
  PARSER_MEDIA_TYPE?: DOMParserSupportedType | undefined;

  /**
   * Return a DOM `DocumentFragment` instead of an HTML string (default is false).
   */
  RETURN_DOM_FRAGMENT?: boolean | undefined;

  /**
   * Return a DOM `HTMLBodyElement` instead of an HTML string (default is false).
   */
  RETURN_DOM?: boolean | undefined;

  /**
   * Return a TrustedHTML object instead of a string if possible.
   */
  RETURN_TRUSTED_TYPE?: boolean | undefined;

  /**
   * Strip `{{ ... }}`, `${ ... }` and `<% ... %>` to make output safe for template systems.
   * Be careful please, this mode is not recommended for production usage.
   * Allowing template parsing in user-controlled HTML is not advised at all.
   * Only use this mode if there is really no alternative.
   */
  SAFE_FOR_TEMPLATES?: boolean | undefined;

  /**
   * Change how e.g. comments containing risky HTML characters are treated.
   * Be very careful, this setting should only be set to `false` if you really only handle
   * HTML and nothing else, no SVG, MathML or the like.
   * Otherwise, changing from `true` to `false` will lead to XSS in this or some other way.
   */
  SAFE_FOR_XML?: boolean | undefined;

  /**
   * Use DOM Clobbering protection on output (default is true, handle with care, minor XSS risks here).
   */
  SANITIZE_DOM?: boolean | undefined;

  /**
   * Enforce strict DOM Clobbering protection via namespace isolation (default is false).
   * When enabled, isolates the namespace of named properties (i.e., `id` and `name` attributes)
   * from JS variables by prefixing them with the string `user-content-`
   */
  SANITIZE_NAMED_PROPS?: boolean | undefined;

  /**
   * Supplied policy must define `createHTML` and `createScriptURL`.
   */
  TRUSTED_TYPES_POLICY?: TrustedTypePolicy | null | undefined;

  /**
   * Controls categories of allowed elements.
   *
   * Note that the `USE_PROFILES` setting will override the `ALLOWED_TAGS` setting
   * so don't use them together.
   */
  USE_PROFILES?: false | UseProfilesConfig | undefined;

  /**
   * Return entire document including <html> tags (default is false).
   */
  WHOLE_DOCUMENT?: boolean | undefined;
}

/**
 * Defines categories of allowed elements.
 */
export interface UseProfilesConfig {
  /**
   * Allow all safe MathML elements.
   */
  mathMl?: boolean | undefined;

  /**
   * Allow all safe SVG elements.
   */
  svg?: boolean | undefined;

  /**
   * Allow all safe SVG Filters.
   */
  svgFilters?: boolean | undefined;

  /**
   * Allow all safe HTML elements.
   */
  html?: boolean | undefined;
}
