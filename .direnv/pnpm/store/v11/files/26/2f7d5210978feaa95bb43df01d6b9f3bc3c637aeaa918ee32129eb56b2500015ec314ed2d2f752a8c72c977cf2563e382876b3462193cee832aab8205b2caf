import { toHex, fromHex } from '@exodus/bytes/hex.js'
import { assert } from './fallback/_utils.js'

const _0n = BigInt(0)

export function fromBigInt(x, { length, format } = {}) {
  assert(Number.isSafeInteger(length) && length > 0, 'Expected length arg to be a positive integer')
  assert(typeof x === 'bigint' && x >= _0n, 'Expected a non-negative bigint')
  const hex = x.toString(16)
  assert(length * 2 >= hex.length, `Can not fit supplied number into ${length} bytes`)
  return fromHex(hex.padStart(length * 2, '0'), format)
}

export const toBigInt = (a) => BigInt('0x' + (toHex(a) || '0'))
