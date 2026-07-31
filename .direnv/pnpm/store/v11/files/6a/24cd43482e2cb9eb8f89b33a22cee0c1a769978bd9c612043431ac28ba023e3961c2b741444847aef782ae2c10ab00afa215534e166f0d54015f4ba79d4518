import { createMultibyteDecoder } from '@exodus/bytes/multi-byte.js'
import { multibyteEncoder } from './fallback/multi-byte.js'
import { setMultibyte } from './fallback/encoding.js'

setMultibyte(createMultibyteDecoder, multibyteEncoder)

export {
  TextDecoder,
  TextEncoder,
  TextDecoderStream,
  TextEncoderStream,
  normalizeEncoding,
  getBOMEncoding,
  labelToName,
  legacyHookDecode,
  isomorphicDecode,
  isomorphicEncode,
} from './fallback/encoding.js'
