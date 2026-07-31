import { assertU8, toBuf } from './fallback/_utils.js'
import { isDeno } from './fallback/platform.js'
import { isAsciiSuperset, multibyteDecoder, multibyteEncoder } from './fallback/multi-byte.js'
import { isAscii } from 'node:buffer'

export function createMultibyteDecoder(encoding, loose = false) {
  const jsDecoder = multibyteDecoder(encoding, loose) // asserts
  let streaming = false
  const asciiSuperset = isAsciiSuperset(encoding)
  return (arr, stream = false) => {
    assertU8(arr)
    if (!streaming) {
      if (arr.byteLength === 0) return ''
      if (asciiSuperset && isAscii(arr)) {
        if (isDeno) return toBuf(arr).toString()
        return toBuf(arr).latin1Slice(0, arr.byteLength) // .latin1Slice is faster than .asciiSlice
      }
    }

    streaming = stream
    return jsDecoder(arr, stream)
  }
}

export function createMultibyteEncoder(encoding, { mode = 'fatal' } = {}) {
  // TODO: replacement, truncate (replacement will need varying length)
  if (mode !== 'fatal') throw new Error('Unsupported mode')
  return multibyteEncoder(encoding) // asserts
}
