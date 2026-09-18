package attachment

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func DataURL(mime string, raw []byte) string {
	if mime == "" {
		mime = "application/octet-stream"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

func ParseDataURL(value string) (mime string, raw []byte, err error) {
	raw, err = decodeDataURL(value, MaxSourceBytes)
	if err != nil {
		return "", nil, err
	}
	const prefix = "data:"
	const marker = ";base64,"
	i := len(prefix)
	j := strings.Index(value, marker)
	if j <= i {
		return "", nil, Error{Code: CodeUnsupported, Message: defaultDetail(CodeUnsupported)}
	}
	return normalizeDeclaredMIME(value[i:j]), raw, nil
}

func FormatImageNote(ref AttachmentRef) string {
	name := ref.DisplayName
	if name == "" {
		name = "image"
	}
	return fmt.Sprintf("[image attachment %s %dx%d %s]", name, ref.Width, ref.Height, ref.MIME())
}
