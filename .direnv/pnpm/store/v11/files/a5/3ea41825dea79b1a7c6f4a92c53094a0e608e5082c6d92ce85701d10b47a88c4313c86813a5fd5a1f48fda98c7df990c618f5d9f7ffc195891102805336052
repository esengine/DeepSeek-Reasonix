import { encodeApi, decodeApiDecoders, decodeApiJS } from './fallback/utf16.js'
import { nativeDecoder } from './fallback/platform.native.js'

function checkDecoders() {
  // Not all barebone engines with TextDecoder support something except utf-8
  // Also workerd specifically has a broken utf-16le implementation
  if (!nativeDecoder) return false
  try {
    const a = new TextDecoder('utf-16le').decode(Uint8Array.of(1, 2, 3, 0xd8))
    const b = new TextDecoder('utf-16be').decode(Uint8Array.of(2, 1, 0xd8, 3))
    return a === b && a === '\u0201\uFFFD'
  } catch {}

  return false
}

const decode = checkDecoders() ? decodeApiDecoders : decodeApiJS

export const utf16fromString = (str, format = 'uint16') => encodeApi(str, false, format)
export const utf16fromStringLoose = (str, format = 'uint16') => encodeApi(str, true, format)
export const utf16toString = (arr, format = 'uint16') => decode(arr, false, format)
export const utf16toStringLoose = (arr, format = 'uint16') => decode(arr, true, format)
