import { seal } from './utils.js';

export const MUSTACHE_EXPR = seal(/{{[\w\W]*|^[\w\W]*}}/g);
export const ERB_EXPR = seal(/<%[\w\W]*|^[\w\W]*%>/g);
export const TMPLIT_EXPR = seal(/\${[\w\W]*/g);
export const DATA_ATTR = seal(/^data-[\-\w.\u00B7-\uFFFF]+$/); // eslint-disable-line no-useless-escape
export const ARIA_ATTR = seal(/^aria-[\-\w]+$/); // eslint-disable-line no-useless-escape
export const IS_ALLOWED_URI = seal(
  /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp|matrix):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i // eslint-disable-line no-useless-escape
);
export const IS_SCRIPT_OR_DATA = seal(/^(?:\w+script|data):/i);
export const ATTR_WHITESPACE = seal(
  /[\u0000-\u0020\u00A0\u1680\u180E\u2000-\u2029\u205F\u3000]/g // eslint-disable-line no-control-regex
);
export const DOCTYPE_NAME = seal(/^html$/i);
export const CUSTOM_ELEMENT = seal(/^[a-z][.\w]*(-[.\w]+)+$/i);

// Markup-significant character probes used by _sanitizeElements.
// Shared module-level instances are safe despite the sticky /g flags:
// unapply() resets lastIndex for RegExp receivers before every call.
export const ELEMENT_MARKUP_PROBE = seal(/<[/\w!]/g);
export const COMMENT_MARKUP_PROBE = seal(/<[/\w]/g);
export const FALLBACK_TAG_CLOSE = seal(/<\/no(script|embed|frames)/i);
export const SELF_CLOSING_TAG = seal(/\/>/i);
