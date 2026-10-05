package productdocs

import "bytes"

// blankMetadataHeader turns the ownership header docs/DOCS_STANDARD.md requires
// into blank lines. Markdown reads `---`, key lines, `---` as a heading, which
// would surface owner names as a section; blanking keeps every offset and line.
func blankMetadataHeader(source []byte) []byte {
	lines := bytes.SplitAfter(source, []byte("\n"))
	if len(lines) == 0 || string(bytes.TrimSpace(lines[0])) != "---" {
		return source
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if string(bytes.TrimSpace(lines[i])) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return source
	}
	out := bytes.Clone(source)
	offset := 0
	for i := 0; i <= end; i++ {
		for j := offset; j < offset+len(lines[i]); j++ {
			if out[j] != '\n' && out[j] != '\r' {
				out[j] = ' '
			}
		}
		offset += len(lines[i])
	}
	return out
}
