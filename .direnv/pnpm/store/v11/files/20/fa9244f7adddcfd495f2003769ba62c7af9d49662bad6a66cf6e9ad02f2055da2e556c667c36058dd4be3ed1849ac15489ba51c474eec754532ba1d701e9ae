/**
 * css-var
 */

import { CSSToken, TokenType, tokenize } from '@csstools/css-tokenizer';
import {
  CacheItem,
  NullObject,
  createCacheKey,
  getCache,
  setCache
} from './cache';
import { isString } from './common';
import { cssCalc } from './css-calc';
import { isColor } from './util';
import { Options } from './typedef';

/* constants */
import { FN_VAR, SYN_FN_CALC, SYN_FN_VAR, VAL_SPEC } from './constant';
const {
  CloseParen: PAREN_CLOSE,
  Comment: COMMENT,
  EOF,
  Ident: IDENT,
  Whitespace: W_SPACE
} = TokenType;
const NAMESPACE = 'css-var';

/* regexp */
const REG_FN_CALC = new RegExp(SYN_FN_CALC);
const REG_FN_VAR = new RegExp(SYN_FN_VAR);
const REG_CSS_WIDE_KEYWORD = /^(?:inherit|initial|revert(?:-layer)?|unset)$/;

/**
 * resolve custom property
 * @param tokens - CSS tokens
 * @param [opt] - options
 * @returns result - [tokens, resolvedValue]
 */
export function resolveCustomProperty(
  tokens: CSSToken[],
  opt: Options = {}
): [CSSToken[], string] {
  if (!Array.isArray(tokens)) {
    throw new TypeError(`${tokens} is not an array.`);
  }
  const { customProperty = {} } = opt;
  const items: string[] = [];
  while (tokens.length) {
    const token = tokens.shift();
    if (!token) {
      break;
    }
    if (!Array.isArray(token)) {
      throw new TypeError(`${token} is not an array.`);
    }
    const [type, value] = token as [TokenType, string];
    // end of var()
    if (type === PAREN_CLOSE) {
      break;
    }
    // nested var()
    if (value === FN_VAR) {
      const [, item] = resolveCustomProperty(tokens, opt);
      if (item) {
        items.push(item);
      }
    } else if (type === IDENT) {
      if (value.startsWith('--')) {
        let item;
        if (Object.hasOwn(customProperty, value)) {
          item = customProperty[value] as string;
        } else if (typeof customProperty.callback === 'function') {
          item = customProperty.callback(value);
        }
        if (item) {
          items.push(item);
        }
      } else if (value) {
        items.push(value);
      }
    }
  }
  let resolveAsColor = false;
  if (items.length > 1) {
    resolveAsColor = isColor(items[items.length - 1]);
  }
  let resolvedValue = '';
  for (let item of items) {
    item = item.trim();
    if (REG_FN_VAR.test(item)) {
      // recurse resolveVar()
      const resolvedItem = resolveVar(item, opt);
      if (isString(resolvedItem)) {
        if (!resolveAsColor || isColor(resolvedItem)) {
          resolvedValue = resolvedItem;
        }
      }
    } else if (REG_FN_CALC.test(item)) {
      item = cssCalc(item, opt);
      if (!resolveAsColor || isColor(item)) {
        resolvedValue = item;
      }
    } else if (item && !REG_CSS_WIDE_KEYWORD.test(item)) {
      if (!resolveAsColor || isColor(item)) {
        resolvedValue = item;
      }
    }
    if (resolvedValue) {
      break;
    }
  }
  return [tokens, resolvedValue];
}

/**
 * parse tokens
 * @param tokens - CSS tokens
 * @param [opt] - options
 * @returns parsed tokens
 */
export function parseTokens(
  tokens: CSSToken[],
  opt: Options = {}
): string[] | NullObject {
  const res: string[] = [];
  while (tokens.length) {
    const token = tokens.shift();
    if (!token) break;
    const [type = '', value = ''] = token as [TokenType, string];
    if (value === FN_VAR) {
      const [, resolvedValue] = resolveCustomProperty(tokens, opt);
      if (!resolvedValue) {
        return new NullObject();
      }
      res.push(resolvedValue);
    } else {
      switch (type) {
        case PAREN_CLOSE: {
          if (res.length) {
            if (res[res.length - 1] === ' ') {
              res[res.length - 1] = value;
            } else {
              res.push(value);
            }
          } else {
            res.push(value);
          }
          break;
        }
        case W_SPACE: {
          if (res.length) {
            const lastValue = res[res.length - 1];
            if (
              isString(lastValue) &&
              !lastValue.endsWith('(') &&
              lastValue !== ' '
            ) {
              res.push(value);
            }
          }
          break;
        }
        default: {
          if (type !== COMMENT && type !== EOF) {
            res.push(value);
          }
        }
      }
    }
  }
  return res;
}

/**
 * resolve CSS var()
 * @param value - CSS value including var()
 * @param [opt] - options
 * @returns resolved value
 */
export function resolveVar(
  value: string,
  opt: Options = {}
): string | NullObject {
  const { format = '' } = opt;
  if (isString(value)) {
    if (!REG_FN_VAR.test(value) || format === VAL_SPEC) {
      return value;
    }
    value = value.trim();
  } else {
    throw new TypeError(`${value} is not a string.`);
  }
  const cacheKey: string = createCacheKey(
    {
      namespace: NAMESPACE,
      name: 'resolveVar',
      value
    },
    opt
  );
  const cachedResult = getCache(cacheKey);
  if (cachedResult instanceof CacheItem) {
    if (cachedResult.isNull) {
      return cachedResult as NullObject;
    }
    return cachedResult.item as string;
  }
  const tokens = tokenize({ css: value });
  const values = parseTokens(tokens, opt);
  if (Array.isArray(values)) {
    let color = values.join('');
    if (REG_FN_CALC.test(color)) {
      color = cssCalc(color, opt);
    }
    setCache(cacheKey, color);
    return color;
  } else {
    setCache(cacheKey, null);
    return new NullObject();
  }
}

/**
 * CSS var()
 * @param value - CSS value including var()
 * @param [opt] - options
 * @returns resolved value
 */
export const cssVar = (value: string, opt: Options = {}): string => {
  const resolvedValue = resolveVar(value, opt);
  if (isString(resolvedValue)) {
    return resolvedValue;
  }
  return '';
};
