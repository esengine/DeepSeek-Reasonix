// We trust browsers to always have correct TextDecoder for utf-16le/utf-16be with ignoreBOM without streaming

import { encodeApi, decodeApiDecoders } from './fallback/utf16.js'

export const utf16fromString = (str, format = 'uint16') => encodeApi(str, false, format)
export const utf16fromStringLoose = (str, format = 'uint16') => encodeApi(str, true, format)
export const utf16toString = (arr, format = 'uint16') => decodeApiDecoders(arr, false, format)
export const utf16toStringLoose = (arr, format = 'uint16') => decodeApiDecoders(arr, true, format)
