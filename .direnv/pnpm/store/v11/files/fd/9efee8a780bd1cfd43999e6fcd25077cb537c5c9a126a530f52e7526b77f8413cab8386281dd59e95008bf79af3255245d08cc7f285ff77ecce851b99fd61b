import { assertEmptyRest } from './assert.js'
import { fromUint8, E_STRING } from './fallback/_utils.js'
import * as js from './fallback/base32.js'

// See https://datatracker.ietf.org/doc/html/rfc4648

// 8 chars per 5 bytes

function fromBase32common(str, mode, padding, format, rest) {
  if (typeof str !== 'string') throw new TypeError(E_STRING)
  if (rest !== null) assertEmptyRest(rest)

  if (padding === true) {
    if (str.length % 8 !== 0) throw new SyntaxError(js.E_PADDING)
  } else if (padding === false) {
    if (str.endsWith('=')) throw new SyntaxError('Did not expect padding in base32 input')
  } else if (padding !== 'both') {
    throw new TypeError('Invalid padding option')
  }

  return fromUint8(js.fromBase32(str, mode), format)
}

// By default, valid padding is accepted but not required
const fromBase32wrap = (mode) => (str, options) => {
  if (!options) return fromBase32common(str, mode, 'both', 'uint8', null)
  const { format = 'uint8', padding = 'both', ...rest } = options
  return fromBase32common(str, mode, padding, format, rest)
}

export const fromBase32 = fromBase32wrap(0)
export const fromBase32hex = fromBase32wrap(1)
export const fromBase32crockford = fromBase32wrap(2)

export const toBase32 = (arr, { padding = false } = {}) => js.toBase32(arr, 0, padding)
export const toBase32hex = (arr, { padding = false } = {}) => js.toBase32(arr, 1, padding)
export const toBase32crockford = (arr, { padding = false } = {}) => js.toBase32(arr, 2, padding)
