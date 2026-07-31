"use strict";
const MIMEType = require("./mime-type.js");

// Normalize a MIME type input (string or MIMEType-like object) to a MIMEType.
// Returns null if parsing fails (including for undefined input).
function normalizeMIMEType(input) {
  return MIMEType.parse(`${input}`);
}

// https://mimesniff.spec.whatwg.org/#xml-mime-type
function isXMLMIMEType(mimeType) {
  return mimeType.subtype.endsWith("+xml") ||
         (mimeType.type === "text" && mimeType.subtype === "xml") ||
         (mimeType.type === "application" && mimeType.subtype === "xml");
}

// https://mimesniff.spec.whatwg.org/#html-mime-type
function isHTMLMIMEType(mimeType) {
  return mimeType.type === "text" && mimeType.subtype === "html";
}

// https://mimesniff.spec.whatwg.org/#resource-header
const RESOURCE_HEADER_LENGTH = 1445;

function getResourceHeader(resource) {
  if (resource.length <= RESOURCE_HEADER_LENGTH) {
    return resource;
  }
  return resource.subarray(0, RESOURCE_HEADER_LENGTH);
}

// https://mimesniff.spec.whatwg.org/#image-mime-type
function isImageMIMEType(mimeType) {
  return mimeType.type === "image";
}

// https://mimesniff.spec.whatwg.org/#audio-or-video-mime-type
function isAudioOrVideoMIMEType(mimeType) {
  return mimeType.type === "audio" ||
         mimeType.type === "video" ||
         (mimeType.type === "application" && mimeType.subtype === "ogg");
}

// https://mimesniff.spec.whatwg.org/#whitespace-byte
function isWhitespaceByte(byte) {
  return byte === 0x09 || byte === 0x0A || byte === 0x0C || byte === 0x0D || byte === 0x20;
}

// https://mimesniff.spec.whatwg.org/#binary-data-byte
function isBinaryDataByte(byte) {
  return (byte >= 0x00 && byte <= 0x08) ||
         byte === 0x0B ||
         (byte >= 0x0E && byte <= 0x1A) ||
         (byte >= 0x1C && byte <= 0x1F);
}

// https://mimesniff.spec.whatwg.org/#pattern-matching-algorithm
function matchesSignature(resource, signature) {
  const { pattern, mask, ignoredLeadingBytes, mimeType } = signature;

  let s = 0;
  if (ignoredLeadingBytes) {
    while (s < resource.length && ignoredLeadingBytes(resource[s])) {
      s++;
    }
  }

  if (resource.length < s + pattern.length) {
    return null;
  }

  for (let i = 0; i < pattern.length; i++) {
    if ((resource[s + i] & mask[i]) !== (pattern[i] & mask[i])) {
      return null;
    }
  }

  return mimeType;
}

// https://mimesniff.spec.whatwg.org/#rules-for-identifying-an-unknown-mime-type
const step1Table = [
  // <!DOCTYPE HTML TT
  {
    pattern: [0x3C, 0x21, 0x44, 0x4F, 0x43, 0x54, 0x59, 0x50, 0x45, 0x20, 0x48, 0x54, 0x4D, 0x4C, 0x20],
    mask: [0xFF, 0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <HTML TT
  {
    pattern: [0x3C, 0x48, 0x54, 0x4D, 0x4C, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <HEAD TT
  {
    pattern: [0x3C, 0x48, 0x45, 0x41, 0x44, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <SCRIPT TT
  {
    pattern: [0x3C, 0x53, 0x43, 0x52, 0x49, 0x50, 0x54, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <IFRAME TT
  {
    pattern: [0x3C, 0x49, 0x46, 0x52, 0x41, 0x4D, 0x45, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <H1 TT
  {
    pattern: [0x3C, 0x48, 0x31, 0x20],
    mask: [0xFF, 0xDF, 0xFF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <DIV TT
  {
    pattern: [0x3C, 0x44, 0x49, 0x56, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <FONT TT
  {
    pattern: [0x3C, 0x46, 0x4F, 0x4E, 0x54, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <TABLE TT
  {
    pattern: [0x3C, 0x54, 0x41, 0x42, 0x4C, 0x45, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <A TT
  {
    pattern: [0x3C, 0x41, 0x20],
    mask: [0xFF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <STYLE TT
  {
    pattern: [0x3C, 0x53, 0x54, 0x59, 0x4C, 0x45, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <TITLE TT
  {
    pattern: [0x3C, 0x54, 0x49, 0x54, 0x4C, 0x45, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <B TT
  {
    pattern: [0x3C, 0x42, 0x20],
    mask: [0xFF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <BODY TT
  {
    pattern: [0x3C, 0x42, 0x4F, 0x44, 0x59, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <BR TT
  {
    pattern: [0x3C, 0x42, 0x52, 0x20],
    mask: [0xFF, 0xDF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <P TT
  {
    pattern: [0x3C, 0x50, 0x20],
    mask: [0xFF, 0xDF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <!-- TT
  {
    pattern: [0x3C, 0x21, 0x2D, 0x2D, 0x20],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xE1],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/html"
  },
  // <?xml
  {
    pattern: [0x3C, 0x3F, 0x78, 0x6D, 0x6C],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    ignoredLeadingBytes: isWhitespaceByte,
    mimeType: "text/xml"
  },
  // %PDF-
  {
    pattern: [0x25, 0x50, 0x44, 0x46, 0x2D],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "application/pdf"
  }
];

// https://mimesniff.spec.whatwg.org/#rules-for-identifying-an-unknown-mime-type
const step2Table = [
  // %!PS-Adobe-
  {
    pattern: [0x25, 0x21, 0x50, 0x53, 0x2D, 0x41, 0x64, 0x6F, 0x62, 0x65, 0x2D],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "application/postscript"
  },
  // UTF-16BE BOM
  {
    pattern: [0xFE, 0xFF, 0x00, 0x00],
    mask: [0xFF, 0xFF, 0x00, 0x00],
    mimeType: "text/plain"
  },
  // UTF-16LE BOM
  {
    pattern: [0xFF, 0xFE, 0x00, 0x00],
    mask: [0xFF, 0xFF, 0x00, 0x00],
    mimeType: "text/plain"
  },
  // UTF-8 BOM
  {
    pattern: [0xEF, 0xBB, 0xBF, 0x00],
    mask: [0xFF, 0xFF, 0xFF, 0x00],
    mimeType: "text/plain"
  }
];

// https://mimesniff.spec.whatwg.org/#matching-an-image-type-pattern
const imageSignatures = [
  { pattern: [0x00, 0x00, 0x01, 0x00], mask: [0xFF, 0xFF, 0xFF, 0xFF], mimeType: "image/x-icon" },
  { pattern: [0x00, 0x00, 0x02, 0x00], mask: [0xFF, 0xFF, 0xFF, 0xFF], mimeType: "image/x-icon" },
  { pattern: [0x42, 0x4D], mask: [0xFF, 0xFF], mimeType: "image/bmp" },
  {
    pattern: [0x47, 0x49, 0x46, 0x38, 0x37, 0x61],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "image/gif"
  },
  {
    pattern: [0x47, 0x49, 0x46, 0x38, 0x39, 0x61],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "image/gif"
  },
  {
    pattern: [0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "image/webp"
  },
  {
    pattern: [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "image/png"
  },
  { pattern: [0xFF, 0xD8, 0xFF], mask: [0xFF, 0xFF, 0xFF], mimeType: "image/jpeg" }
];

// https://mimesniff.spec.whatwg.org/#matching-an-audio-or-video-type-pattern
const audioVideoSignatures = [
  {
    pattern: [0x46, 0x4F, 0x52, 0x4D, 0x00, 0x00, 0x00, 0x00, 0x41, 0x49, 0x46, 0x46],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "audio/aiff"
  },
  { pattern: [0x49, 0x44, 0x33], mask: [0xFF, 0xFF, 0xFF], mimeType: "audio/mpeg" },
  {
    pattern: [0x4F, 0x67, 0x67, 0x53, 0x00],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "application/ogg"
  },
  {
    pattern: [0x4D, 0x54, 0x68, 0x64, 0x00, 0x00, 0x00, 0x06],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "audio/midi"
  },
  {
    pattern: [0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x41, 0x56, 0x49, 0x20],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "video/avi"
  },
  {
    pattern: [0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x41, 0x56, 0x45],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "audio/wave"
  }
];

// https://mimesniff.spec.whatwg.org/#signature-for-mp4
function matchMP4(resource) {
  if (resource.length < 12) {
    return null;
  }
  // Bytes 4-7 must be "ftyp"
  if (resource[4] !== 0x66 || resource[5] !== 0x74 ||
      resource[6] !== 0x79 || resource[7] !== 0x70) {
    return null;
  }
  const length = (resource[0] << 24) | (resource[1] << 16) | (resource[2] << 8) | resource[3];
  if (length < 12 || length > resource.length) {
    return null;
  }
  const brand = String.fromCharCode(resource[8], resource[9], resource[10], resource[11]);
  const mp4Brands = ["mp41", "mp42", "isom", "iso2", "mmp4", "M4V ", "M4A ", "M4P ", "avc1"];
  if (mp4Brands.includes(brand)) {
    return "video/mp4";
  }
  for (let i = 16; i + 4 <= length && i + 4 <= resource.length; i += 4) {
    const compat = String.fromCharCode(resource[i], resource[i + 1], resource[i + 2], resource[i + 3]);
    if (mp4Brands.includes(compat)) {
      return "video/mp4";
    }
  }
  return null;
}

// https://mimesniff.spec.whatwg.org/#parse-a-vint
//
// Note: The spec has a bug: it takes an "iter" parameter but never uses it, always starting from index 0.
// See https://github.com/whatwg/mimesniff/issues/167. We implement the intended behavior.
//
// The spec algorithm also does extra processing which is not actually needed by its single caller:
// https://github.com/whatwg/mimesniff/issues/146. We omit that processing.
function parseVint(sequence, iter) {
  let mask = 128;
  const maxVintLength = 8;
  let numberSize = 1;
  while (numberSize < maxVintLength && numberSize < sequence.length) {
    if ((sequence[iter] & mask) !== 0) {
      break;
    }
    mask >>= 1;
    ++numberSize;
  }
  return numberSize;
}

// https://mimesniff.spec.whatwg.org/#matching-a-padded-sequence
function matchPaddedSequence(sequence, offset, pattern) {
  // Skip leading 0x00 bytes
  while (offset < sequence.length && sequence[offset] === 0x00) {
    offset++;
  }
  // Check if pattern matches at current offset
  if (sequence.length < offset + pattern.length) {
    return false;
  }
  for (let i = 0; i < pattern.length; i++) {
    if (sequence[offset + i] !== pattern[i]) {
      return false;
    }
  }
  return true;
}

// https://mimesniff.spec.whatwg.org/#signature-for-webm
function matchWebM(resource) {
  const { length } = resource;
  // Step 3: If length < 4, return false
  if (length < 4) {
    return null;
  }
  // Step 4: Check EBML header 0x1A 0x45 0xDF 0xA3
  if (resource[0] !== 0x1A || resource[1] !== 0x45 ||
      resource[2] !== 0xDF || resource[3] !== 0xA3) {
    return null;
  }
  // Step 5-6: Search for DocType element (0x42 0x82) in bytes 4-37
  let iter = 4;
  while (iter < length && iter < 38) {
    if (iter + 1 < length && resource[iter] === 0x42 && resource[iter + 1] === 0x82) {
      iter += 2;
      if (iter >= length) {
        break;
      }
      const numberSize = parseVint(resource, iter);
      iter += numberSize;
      if (iter >= length - 4) {
        break;
      }
      // Match padded sequence "webm" (0x77 0x65 0x62 0x6D)
      if (matchPaddedSequence(resource, iter, [0x77, 0x65, 0x62, 0x6D])) {
        return "video/webm";
      }
    }
    iter++;
  }
  return null;
}

// https://mimesniff.spec.whatwg.org/#signature-for-mp3-without-id3

// Bitrate tables (kbps) indexed by bitrate-index (0-15)
// https://mimesniff.spec.whatwg.org/#mp3-rates-table
const mp3Rates = [0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0];
const mp25Rates = [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0];

// Sample rate table indexed by samplerate-index (0-3)
// https://mimesniff.spec.whatwg.org/#mp3-sample-rate-table
const sampleRates = [44100, 48000, 32000, 0];

// https://mimesniff.spec.whatwg.org/#match-an-mp3-header
function matchMP3Header(sequence, s) {
  const { length } = sequence;
  // Step 1: If length < s + 4, return false
  if (length < s + 4) {
    return false;
  }
  // Step 2: If sequence[s] ≠ 0xFF or sequence[s+1] & 0xE0 ≠ 0xE0, return false
  if (sequence[s] !== 0xFF || (sequence[s + 1] & 0xE0) !== 0xE0) {
    return false;
  }
  // Step 3: Extract layer
  const layer = (sequence[s + 1] & 0x06) >> 1;
  // Step 4: If layer is 0, return false
  if (layer === 0) {
    return false;
  }
  // Step 5: Extract bit-rate index, return false if 15
  const bitRate = (sequence[s + 2] & 0xF0) >> 4;
  if (bitRate === 15) {
    return false;
  }
  // Step 6: Extract sample-rate index, return false if 3
  const sampleRate = (sequence[s + 2] & 0x0C) >> 2;
  if (sampleRate === 3) {
    return false;
  }
  // Step 9: Check final-layer (layer must be 3 for MP3)
  const finalLayer = (4 - layer) & 0x03;
  if (finalLayer !== 3) {
    return false;
  }
  // Step 10: Return true
  return true;
}

// https://mimesniff.spec.whatwg.org/#parse-an-mp3-frame
function parseMP3Frame(sequence, s) {
  // Step 1: Extract version
  const version = (sequence[s + 1] & 0x18) >> 3;
  // Step 2: Extract bitrate-index
  const bitrateIndex = (sequence[s + 2] & 0xF0) >> 4;
  // Step 3+4: Get bitrate from appropriate table
  const bitrate = (version & 0x01) !== 0 ? mp3Rates[bitrateIndex] : mp25Rates[bitrateIndex];
  // Step 5: Extract samplerate-index
  const samplerateIndex = (sequence[s + 2] & 0x0C) >> 2;
  // Step 6: Get samplerate
  const samplerate = sampleRates[samplerateIndex];
  // Step 7: Extract pad
  const pad = (sequence[s + 2] & 0x02) >> 1;

  return { version, bitrate, samplerate, pad };
}

// https://mimesniff.spec.whatwg.org/#compute-an-mp3-frame-size
function computeMP3FrameSize(version, bitrate, samplerate, pad) {
  // Step 1: Determine scale based on version
  const scale = version === 1 ? 72 : 144;
  // Step 2: Compute size
  let size = Math.floor((bitrate * 1000 * scale) / samplerate);
  // Step 3: Add padding if present
  if (pad !== 0) {
    size += 1;
  }
  // Step 4: Return size
  return size;
}

// https://mimesniff.spec.whatwg.org/#signature-for-mp3-without-id3
function matchMP3WithoutID3(resource) {
  const { length } = resource;
  // Step 2: Let s be 0
  let s = 0;
  // Step 3: If match mp3 header returns false, return false
  if (!matchMP3Header(resource, s)) {
    return null;
  }
  // Step 4: Parse an mp3 frame
  const { version, bitrate, samplerate, pad } = parseMP3Frame(resource, s);
  // Step 5: Compute frame size
  const skippedBytes = computeMP3FrameSize(version, bitrate, samplerate, pad);
  // Step 6: If skipped-bytes < 4 or skipped-bytes > length - s, return false
  if (skippedBytes < 4 || skippedBytes > length - s) {
    return null;
  }
  // Step 7: Increment s by skipped-bytes
  s += skippedBytes;
  // Step 8: If match mp3 header returns false, return false; otherwise return true
  if (!matchMP3Header(resource, s)) {
    return null;
  }
  return "audio/mpeg";
}

// https://mimesniff.spec.whatwg.org/#matching-an-archive-type-pattern
const archiveSignatures = [
  { pattern: [0x1F, 0x8B, 0x08], mask: [0xFF, 0xFF, 0xFF], mimeType: "application/x-gzip" },
  { pattern: [0x50, 0x4B, 0x03, 0x04], mask: [0xFF, 0xFF, 0xFF, 0xFF], mimeType: "application/zip" },
  {
    pattern: [0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00],
    mask: [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF],
    mimeType: "application/x-rar-compressed"
  }
];

// https://mimesniff.spec.whatwg.org/#matching-an-image-type-pattern
function matchImageType(resource) {
  for (const sig of imageSignatures) {
    const result = matchesSignature(resource, sig);
    if (result) {
      return result;
    }
  }
  return null;
}

// https://mimesniff.spec.whatwg.org/#matching-an-audio-or-video-type-pattern
function matchAudioOrVideoType(resource) {
  for (const sig of audioVideoSignatures) {
    const result = matchesSignature(resource, sig);
    if (result) {
      return result;
    }
  }
  const mp4Result = matchMP4(resource);
  if (mp4Result) {
    return mp4Result;
  }
  const webmResult = matchWebM(resource);
  if (webmResult) {
    return webmResult;
  }
  const mp3Result = matchMP3WithoutID3(resource);
  if (mp3Result) {
    return mp3Result;
  }
  return null;
}

// https://mimesniff.spec.whatwg.org/#rules-for-text-or-binary
function distinguishTextOrBinary(resourceHeader) {
  // Step 1: Let length be the number of bytes in the resource header.
  const { length } = resourceHeader;

  // Step 2: If the first 2 bytes match a UTF-16 BOM, return "text/plain".
  if (length >= 2) {
    // UTF-16 BE BOM
    if (resourceHeader[0] === 0xFE && resourceHeader[1] === 0xFF) {
      return "text/plain";
    }
    // UTF-16 LE BOM
    if (resourceHeader[0] === 0xFF && resourceHeader[1] === 0xFE) {
      return "text/plain";
    }
  }

  // Step 3: If the first 3 bytes match the UTF-8 BOM, return "text/plain".
  if (length >= 3) {
    if (resourceHeader[0] === 0xEF && resourceHeader[1] === 0xBB && resourceHeader[2] === 0xBF) {
      return "text/plain";
    }
  }

  // Step 4: If the resource header contains no binary data bytes, return "text/plain".
  for (let i = 0; i < length; i++) {
    if (isBinaryDataByte(resourceHeader[i])) {
      // Step 5: Return "application/octet-stream".
      return "application/octet-stream";
    }
  }

  return "text/plain";
}

// https://mimesniff.spec.whatwg.org/#rules-for-identifying-an-unknown-mime-type
function identifyAnUnknownMIMEType(resourceHeader, { sniffScriptable = false } = {}) {
  // Step 1
  if (sniffScriptable) {
    for (const sig of step1Table) {
      const result = matchesSignature(resourceHeader, sig);
      if (result) {
        return result;
      }
    }
  }

  // Step 2
  for (const sig of step2Table) {
    const result = matchesSignature(resourceHeader, sig);
    if (result) {
      return result;
    }
  }

  // Step 3: image type pattern matching
  for (const sig of imageSignatures) {
    const result = matchesSignature(resourceHeader, sig);
    if (result) {
      return result;
    }
  }

  // Step 4: audio/video type pattern matching
  // https://mimesniff.spec.whatwg.org/#matching-an-audio-or-video-type-pattern
  for (const sig of audioVideoSignatures) {
    const result = matchesSignature(resourceHeader, sig);
    if (result) {
      return result;
    }
  }
  // Then check MP4, WebM, and MP3-without-ID3 signatures
  const mp4Result = matchMP4(resourceHeader);
  if (mp4Result) {
    return mp4Result;
  }
  const webmResult = matchWebM(resourceHeader);
  if (webmResult) {
    return webmResult;
  }
  const mp3Result = matchMP3WithoutID3(resourceHeader);
  if (mp3Result) {
    return mp3Result;
  }

  // Step 5: archive type pattern matching
  for (const sig of archiveSignatures) {
    const result = matchesSignature(resourceHeader, sig);
    if (result) {
      return result;
    }
  }

  // Step 6: If resource header contains no binary data bytes, return text/plain
  for (let i = 0; i < resourceHeader.length; i++) {
    if (isBinaryDataByte(resourceHeader[i])) {
      // Step 7: return application/octet-stream
      return "application/octet-stream";
    }
  }

  return "text/plain";
}

// Apache bug values that trigger text/binary sniffing
// https://mimesniff.spec.whatwg.org/#supplied-mime-type-detection-algorithm
const apacheBugValues = new Set([
  "text/plain",
  "text/plain; charset=ISO-8859-1",
  "text/plain; charset=iso-8859-1",
  "text/plain; charset=UTF-8"
]);

// https://mimesniff.spec.whatwg.org/#supplied-mime-type-detection-algorithm
function detectSuppliedMIMEType({ contentTypeHeader, providedType }) {
  let suppliedMIMEType = null;
  let checkForApacheBug = false;

  if (contentTypeHeader !== undefined) {
    // Step 2: HTTP Content-Type header
    suppliedMIMEType = normalizeMIMEType(contentTypeHeader);
    if (suppliedMIMEType !== null && typeof contentTypeHeader === "string") {
      checkForApacheBug = apacheBugValues.has(contentTypeHeader);
    }
  } else if (providedType !== undefined) {
    // Steps 3-4: Filesystem or other protocol
    suppliedMIMEType = normalizeMIMEType(providedType);
  }
  // Step 5: If parsing failed, suppliedMIMEType remains null (undefined per spec)

  return { suppliedMIMEType, checkForApacheBug };
}

/**
 * Determine the computed MIME type of a resource.
 * https://mimesniff.spec.whatwg.org/#determining-the-computed-mime-type-of-a-resource
 *
 * @param {Uint8Array} resource - The resource bytes
 * @param {object} options - Options object
 * @param {string} options.contentTypeHeader - The Content-Type header value (for HTTP resources)
 * @param {string} options.providedType - MIME type from filesystem or other protocol (for non-HTTP resources)
 * @param {boolean} options.noSniff - Whether the X-Content-Type-Options: nosniff header was present
 * @param {function} options.isSupported - Predicate to check if an image/audio/video MIME type is supported
 * @returns {MIMEType} The computed MIME type
 */
module.exports = function computedMIMEType(
  resource,
  { contentTypeHeader, providedType, noSniff = false, isSupported = () => true } = {}
) {
  const resourceHeader = getResourceHeader(resource);
  const { suppliedMIMEType, checkForApacheBug } = detectSuppliedMIMEType({ contentTypeHeader, providedType });

  // Step 1: If the supplied MIME type is an XML MIME type or HTML MIME type, return it
  if (suppliedMIMEType !== null && (isXMLMIMEType(suppliedMIMEType) || isHTMLMIMEType(suppliedMIMEType))) {
    return suppliedMIMEType;
  }

  // Step 2: If supplied MIME type is undefined, or its essence is "unknown/unknown",
  // "application/unknown", or "*/*", execute rules for identifying an unknown MIME type
  if (suppliedMIMEType === null ||
      suppliedMIMEType.essence === "unknown/unknown" ||
      suppliedMIMEType.essence === "application/unknown" ||
      suppliedMIMEType.essence === "*/*") {
    // sniff-scriptable flag is the inverse of no-sniff flag
    return new MIMEType(identifyAnUnknownMIMEType(resourceHeader, { sniffScriptable: !noSniff }));
  }

  // Step 3: If the no-sniff flag is set, return the supplied MIME type
  if (noSniff) {
    return suppliedMIMEType;
  }

  // Step 4: If the check-for-apache-bug flag is set, execute rules for distinguishing
  // if a resource is text or binary
  if (checkForApacheBug) {
    return new MIMEType(distinguishTextOrBinary(resourceHeader));
  }

  // Steps 5-6: If supplied MIME type is a supported image MIME type, execute image pattern matching
  if (isImageMIMEType(suppliedMIMEType) && isSupported(suppliedMIMEType)) {
    const imageResult = matchImageType(resourceHeader);
    if (imageResult !== null) {
      return new MIMEType(imageResult);
    }
  }

  // Steps 7-8: If supplied MIME type is a supported audio/video type, execute audio/video matching
  if (isAudioOrVideoMIMEType(suppliedMIMEType) && isSupported(suppliedMIMEType)) {
    const avResult = matchAudioOrVideoType(resourceHeader);
    if (avResult !== null) {
      return new MIMEType(avResult);
    }
  }

  // Step 9: Return the supplied MIME type
  return suppliedMIMEType;
};
