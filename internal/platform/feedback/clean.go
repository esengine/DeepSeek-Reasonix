package feedback

import "encoding/binary"

// metadataFree reports whether an encoded image holds only what its pixels
// need. The service rejects anything else, so a re-encode that somehow kept a
// block is caught here rather than after the upload.
func metadataFree(data []byte, mime string) bool {
	if mime == "image/png" {
		return pngClean(data)
	}
	return jpegClean(data)
}

func pngClean(data []byte) bool {
	const sig = "\x89PNG\r\n\x1a\n"
	if len(data) < len(sig) || string(data[:len(sig)]) != sig {
		return false
	}
	for pos := len(sig); pos+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[pos:]))
		kind := string(data[pos+4 : pos+8])
		switch kind {
		case "IHDR", "PLTE", "tRNS", "IDAT":
		case "IEND":
			return pos+12+size == len(data)
		default:
			return false
		}
		pos += 12 + size
	}
	return false
}

func jpegClean(data []byte) bool {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return false
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xFF {
			return false
		}
		marker := data[pos+1]
		if marker >= 0xE0 && marker <= 0xEF || marker == 0xFE {
			return false
		}
		if marker == 0xDA {
			return true
		}
		pos += 2 + int(binary.BigEndian.Uint16(data[pos+2:]))
	}
	return false
}
