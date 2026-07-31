import { assertU8, fromUint8 } from './fallback/_utils.js'
import * as js from './fallback/hex.js'

const { toHex: webHex } = Uint8Array.prototype // Modern engines have this

export function toHex(arr) {
  assertU8(arr)
  if (arr.length === 0) return ''
  if (webHex && arr.toHex === webHex) return arr.toHex()
  return js.toHex(arr)
}

// Unlike Buffer.from(), throws on invalid input
export const fromHex = Uint8Array.fromHex
  ? (str, format = 'uint8') => fromUint8(Uint8Array.fromHex(str), format)
  : (str, format = 'uint8') => fromUint8(js.fromHex(str), format)
