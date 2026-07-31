/**
 * constant.js
 */

/* string */
export const ATRULE = 'Atrule';
export const ATTR_SELECTOR = 'AttributeSelector';
export const CLASS_SELECTOR = 'ClassSelector';
export const COMBINATOR = 'Combinator';
export const IDENT = 'Identifier';
export const ID_SELECTOR = 'IdSelector';
export const NOT_SUPPORTED_ERR = 'NotSupportedError';
export const NTH = 'Nth';
export const OPERATOR = 'Operator';
export const PS_CLASS_SELECTOR = 'PseudoClassSelector';
export const PS_ELEMENT_SELECTOR = 'PseudoElementSelector';
export const RULE = 'Rule';
export const SCOPE = 'Scope';
export const SELECTOR = 'Selector';
export const SELECTOR_LIST = 'SelectorList';
export const STRING = 'String';
export const SYNTAX_ERR = 'SyntaxError';
export const TARGET_ALL = 'all';
export const TARGET_FIRST = 'first';
export const TARGET_LINEAL = 'lineal';
export const TARGET_SELF = 'self';
export const TYPE_SELECTOR = 'TypeSelector';

/* numeric */
export const BIT_01 = 1;
export const BIT_02 = 2;
export const BIT_04 = 4;
export const BIT_08 = 8;
export const BIT_16 = 0x10;
export const BIT_32 = 0x20;
export const BIT_FFFF = 0xffff;
export const DUO = 2;
export const HEX = 16;
export const TYPE_FROM = 8;
export const TYPE_TO = -1;

/* Node */
export const ELEMENT_NODE = 1;
export const TEXT_NODE = 3;
export const DOCUMENT_NODE = 9;
export const DOCUMENT_FRAGMENT_NODE = 11;
export const DOCUMENT_POSITION_PRECEDING = 2;
export const DOCUMENT_POSITION_CONTAINS = 8;
export const DOCUMENT_POSITION_CONTAINED_BY = 0x10;

/* NodeFilter */
export const SHOW_ALL = 0xffffffff;
export const SHOW_CONTAINER = 0x501;
export const SHOW_DOCUMENT = 0x100;
export const SHOW_DOCUMENT_FRAGMENT = 0x400;
export const SHOW_ELEMENT = 1;

/* selectors */
export const ALPHA_NUM = '[A-Z\\d]+';
export const CHILD_IDX = '(?:first|last|only)-(?:child|of-type)';
export const DIGIT = '(?:0|[1-9]\\d*)';
export const LANG_PART = `(?:-${ALPHA_NUM})*`;
export const PSEUDO_CLASS = `(?:any-)?link|${CHILD_IDX}|checked|empty|indeterminate|read-(?:only|write)|target`;
export const ANB = `[+-]?(?:${DIGIT}n?|n)|(?:[+-]?${DIGIT})?n\\s*[+-]\\s*${DIGIT}`;
// combinators
export const COMBO = '\\s?[\\s>~+]\\s?';
export const DESCEND = '\\s?[\\s>]\\s?';
export const SIBLING = '\\s?[+~]\\s?';
// LOGIC_IS: :is()
export const LOGIC_IS = `:is\\(\\s*[^)]+\\s*\\)`;
// N_TH: excludes An+B with selector list, e.g. :nth-child(2n+1 of .foo)
export const N_TH = `nth-(?:last-)?(?:child|of-type)\\(\\s*(?:even|odd|${ANB})\\s*\\)`;
// SUB_TYPE: attr, id, class, pseudo-class, note that [foo|=bar] is excluded
export const SUB_TYPE = '\\[[^|\\]]+\\]|[#.:][\\w-]+';
export const SUB_TYPE_WO_PSEUDO = '\\[[^|\\]]+\\]|[#.][\\w-]+';
// TAG_TYPE: *, tag
export const TAG_TYPE = '\\*|[A-Za-z][\\w-]*';
export const TAG_TYPE_I = '\\*|[A-Z][\\w-]*';
export const COMPOUND = `(?:${TAG_TYPE}|(?:${TAG_TYPE})?(?:${SUB_TYPE})+)`;
export const COMPOUND_L = `(?:${TAG_TYPE}|(?:${TAG_TYPE})?(?:${SUB_TYPE}|${LOGIC_IS})+)`;
export const COMPOUND_I = `(?:${TAG_TYPE_I}|(?:${TAG_TYPE_I})?(?:${SUB_TYPE})+)`;
export const COMPOUND_WO_PSEUDO = `(?:${TAG_TYPE}|(?:${TAG_TYPE})?(?:${SUB_TYPE_WO_PSEUDO})+)`;
export const COMPLEX = `${COMPOUND}(?:${COMBO}${COMPOUND})*`;
export const COMPLEX_L = `${COMPOUND_L}(?:${COMBO}${COMPOUND_L})*`;
export const HAS_COMPOUND = `has\\([\\s>]?\\s*${COMPOUND_WO_PSEUDO}\\s*\\)`;
export const LOGIC_COMPOUND = `(?:is|not)\\(\\s*${COMPOUND_L}(?:\\s*,\\s*${COMPOUND_L})*\\s*\\)`;
export const LOGIC_COMPLEX = `(?:is|not)\\(\\s*${COMPLEX_L}(?:\\s*,\\s*${COMPLEX_L})*\\s*\\)`;

/* forms and input types */
export const FORM_PARTS = Object.freeze([
  'button',
  'input',
  'select',
  'textarea'
]);
export const INPUT_BUTTON = Object.freeze(['button', 'reset', 'submit']);
export const INPUT_CHECK = Object.freeze(['checkbox', 'radio']);
export const INPUT_DATE = Object.freeze([
  'date',
  'datetime-local',
  'month',
  'time',
  'week'
]);
export const INPUT_TEXT = Object.freeze([
  'email',
  'password',
  'search',
  'tel',
  'text',
  'url'
]);
export const INPUT_EDIT = Object.freeze([
  ...INPUT_DATE,
  ...INPUT_TEXT,
  'number'
]);
export const INPUT_LTR = Object.freeze([
  ...INPUT_CHECK,
  'color',
  'date',
  'image',
  'number',
  'range',
  'time'
]);

/* logical combination pseudo-classes */
export const KEYS_LOGICAL = new Set(['has', 'is', 'not', 'where']);
