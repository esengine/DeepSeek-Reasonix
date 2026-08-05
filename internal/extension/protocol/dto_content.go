package protocol

// Content DTOs: reading externalized content refs back from the host. Large
// externalizable payloads travel as content refs; the extension pages them
// through host/content/read.

// ExternalizedField is the envelope descriptor for one externalizable field
// whose inline value exceeded ExternalizeFieldBytes: the field travels as null
// in its owner document and this descriptor names the content ref holding the
// real bytes. It is an optional addition to the owner DTOs that carry
// externalizable fields (InterceptParams, InterceptResult, EventParams), so
// peers predating it simply never externalize.
type ExternalizedField struct {
	JSONPointer string `json:"jsonPointer" validate:"nonempty"`
	ContentRef  string `json:"contentRef" validate:"nonempty"`
	TotalBytes  int64  `json:"totalBytes" validate:"min=0"`
	SHA256      string `json:"sha256" validate:"sha256"`
}

// ContentReadParams pages one content ref. Offset is a byte position; the
// host answers with at most ContentRefChunkBytes of Base64-decoded data.
type ContentReadParams struct {
	ContentRef string `json:"contentRef" validate:"nonempty"`
	Offset     int64  `json:"offset" validate:"min=0"`
}

// ContentReadResult is one page of content ref data. NextOffset is null at
// end of object; TotalBytes and SHA256 describe the whole object so the
// extension can verify the reassembled bytes.
type ContentReadResult struct {
	ContentRef string          `json:"contentRef" validate:"nonempty"`
	Offset     int64           `json:"offset" validate:"min=0"`
	DataBase64 string          `json:"dataBase64"`
	NextOffset *int64          `json:"nextOffset,omitempty" validate:"min=0"`
	TotalBytes int64           `json:"totalBytes" validate:"min=0"`
	SHA256     string          `json:"sha256" validate:"sha256"`
	Encoding   ContentEncoding `json:"encoding"`
}
