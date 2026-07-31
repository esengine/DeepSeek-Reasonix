import { assertU8 } from './fallback/_utils.js'
import { multibyteDecoder, multibyteEncoder } from './fallback/multi-byte.js'

export function createMultibyteDecoder(encoding, loose = false) {
  const jsDecoder = multibyteDecoder(encoding, loose) // asserts
  let streaming = false
  return (arr, stream = false) => {
    assertU8(arr)
    if (!streaming && arr.byteLength === 0) return ''
    streaming = stream
    return jsDecoder(arr, stream)
  }
}

export function createMultibyteEncoder(encoding, { mode = 'fatal' } = {}) {
  // TODO: replacement, truncate (replacement will need varying length)
  if (mode !== 'fatal') throw new Error('Unsupported mode')
  return multibyteEncoder(encoding) // asserts
}
