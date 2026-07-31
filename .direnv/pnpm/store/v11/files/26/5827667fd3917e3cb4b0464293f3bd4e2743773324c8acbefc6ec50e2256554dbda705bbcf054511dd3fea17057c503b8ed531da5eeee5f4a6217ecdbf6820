import { toBase58, fromBase58 } from '@exodus/bytes/base58.js'
import { assertU8, fromUint8 } from './_utils.js'

const E_CHECKSUM = 'Invalid checksum'

// checksum length is 4, i.e. only the first 4 bytes of the hash are used

function encodeWithChecksum(arr, checksum) {
  // arr type in already validated in input
  const res = new Uint8Array(arr.length + 4)
  res.set(arr, 0)
  res.set(checksum.slice(0, 4), arr.length)
  return toBase58(res)
}

function decodeWithChecksum(str) {
  const arr = fromBase58(str) // checks input
  const payloadSize = arr.length - 4
  if (payloadSize < 0) throw new Error(E_CHECKSUM)
  return [arr.slice(0, payloadSize), arr.slice(payloadSize)]
}

function assertChecksum(c, r) {
  if ((c[0] ^ r[0]) | (c[1] ^ r[1]) | (c[2] ^ r[2]) | (c[3] ^ r[3])) throw new Error(E_CHECKSUM)
}

export const makeBase58check = (hashAlgo, hashAlgoSync) => {
  const apis = {
    async encode(arr) {
      assertU8(arr)
      return encodeWithChecksum(arr, await hashAlgo(arr))
    },
    async decode(str, format = 'uint8') {
      const [payload, checksum] = decodeWithChecksum(str)
      assertChecksum(checksum, await hashAlgo(payload))
      return fromUint8(payload, format)
    },
  }
  if (!hashAlgoSync) return apis
  return {
    ...apis,
    encodeSync(arr) {
      assertU8(arr)
      return encodeWithChecksum(arr, hashAlgoSync(arr))
    },
    decodeSync(str, format = 'uint8') {
      const [payload, checksum] = decodeWithChecksum(str)
      assertChecksum(checksum, hashAlgoSync(payload))
      return fromUint8(payload, format)
    },
  }
}
